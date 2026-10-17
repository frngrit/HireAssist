package objectstore_test

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/frngrit/HireAssist/services/hiring/internal/adapter/objectstore"
	"github.com/frngrit/HireAssist/services/hiring/internal/usecase"
)

var _ usecase.ResumeStore = (*objectstore.Store)(nil)

// A resume goes into a bucket the store creates itself, readable byte for byte as a PDF.
// Runs against the storage named by HIRING_TEST_S3_ENDPOINT, with HIRING_TEST_S3_ACCESS_KEY and
// HIRING_TEST_S3_SECRET_KEY; without it the test is skipped. `docker compose up -d minio`
// provides one.
func TestStoreResume(t *testing.T) {
	endpoint := os.Getenv("HIRING_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("HIRING_TEST_S3_ENDPOINT not set; skipping object storage integration test")
	}
	cfg := objectstore.Config{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("HIRING_TEST_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("HIRING_TEST_S3_SECRET_KEY"),
		Bucket:    "hiring-test-" + time.Now().UTC().Format("20060102150405"),
	}
	store, err := objectstore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	content := []byte("%PDF-1.7\n% a test resume\n%%EOF")

	if err := store.StoreResume(ctx, "jo-1/sb-1/rs-1.pdf", content); err != nil {
		t.Fatalf("first upload, creating the bucket: %v", err)
	}
	if err := store.StoreResume(ctx, "jo-1/sb-1/rs-2.pdf", content); err != nil {
		t.Fatalf("second upload: %v", err)
	}

	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, "")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, key := range []string{"jo-1/sb-1/rs-1.pdf", "jo-1/sb-1/rs-2.pdf"} {
			_ = client.RemoveObject(ctx, cfg.Bucket, key, minio.RemoveObjectOptions{})
		}
		_ = client.RemoveBucket(ctx, cfg.Bucket)
	})
	obj, err := client.GetObject(ctx, cfg.Bucket, "jo-1/sb-1/rs-1.pdf", minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(obj)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := obj.Stat()
	if string(got) != string(content) || info.ContentType != "application/pdf" {
		t.Errorf("stored %q as %q", got, info.ContentType)
	}
}
