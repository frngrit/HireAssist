// Package objectstore keeps resume files in S3-compatible object storage — MinIO when run
// locally (ADR-003: files in object storage, not in either database). The Hiring Service stores
// a file once, when its batch is submitted; the Resume Processing Service reads it from here.
package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config locates the storage and the bucket resumes go in.
type Config struct {
	Endpoint  string // host:port
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

// Store uploads resume files (usecase.ResumeStore).
type Store struct {
	client *minio.Client
	bucket string

	mu     sync.Mutex
	exists bool // the bucket is known to exist
}

// New returns a store for cfg. It does not connect: the first upload does, so the service
// starts while storage is down and only submitting a batch fails.
func New(cfg Config) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("object storage at %q: %w", cfg.Endpoint, err)
	}
	return &Store{client: client, bucket: cfg.Bucket}, nil
}

// StoreResume uploads one resume under key, creating the bucket on first use.
func (s *Store) StoreResume(ctx context.Context, key string, content []byte) error {
	if err := s.ensureBucket(ctx); err != nil {
		return err
	}
	if _, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{ContentType: "application/pdf"}); err != nil {
		return fmt.Errorf("storing %s: %w", key, err)
	}
	return nil
}

func (s *Store) ensureBucket(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exists {
		return nil
	}
	found, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("checking bucket %s: %w", s.bucket, err)
	}
	if !found {
		err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
		// Another instance may have created it in between; that is as good.
		if code := minio.ToErrorResponse(err).Code; err != nil && code != "BucketAlreadyOwnedByYou" && code != "BucketAlreadyExists" {
			return fmt.Errorf("creating bucket %s: %w", s.bucket, err)
		}
	}
	s.exists = true
	return nil
}
