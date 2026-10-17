import { Alert, Badge, Group, Paper, Progress, Stack, Text, Title, Tooltip } from '@mantine/core'
import { Dropzone, MIME_TYPES } from '@mantine/dropzone'
import { notifications } from '@mantine/notifications'
import { IconAlertTriangle, IconFileTypePdf, IconInfoCircle, IconUpload, IconX } from '@tabler/icons-react'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ApiError } from '../../api/client'
import { getBatch, getRankedResults, submitScreeningBatch } from '../../api/endpoints'
import type { JobOpening, ScreeningBatch } from '../../api/types'
import { useI18n } from '../../i18n/I18nProvider'
import { CandidateDrawer } from './CandidateDrawer'
import { POLL_MS } from './poll'
import { ResultsTable } from './ResultsTable'

/** UC-2: submit a batch, watch it progress, and work the ranked list as results arrive. */
export function ScreeningTab({ opening }: { opening: JobOpening }) {
  const { t } = useI18n()
  const [searchParams, setSearchParams] = useSearchParams()
  const resumeId = searchParams.get('resume')
  const [uploaded, remember] = useUploadedBatches(opening.id)

  // The unfiltered ranking: the same query the table runs with no filter, so it is fetched once.
  // It names every batch that has produced a result, and why each unreadable resume was set apart.
  const all = useQuery({
    queryKey: ['results', opening.id, 0, null],
    queryFn: () => getRankedResults(opening.id),
    retry: (count, error) => !(error instanceof ApiError && error.notServed) && count < 3,
  })

  // The Hiring Service has no operation that lists an opening's batches, so the list is the
  // batches the results mention plus the ones submitted from this browser, each asked for by id.
  const batchIds = useMemo(() => {
    const fromResults = [...(all.data?.ranked ?? []), ...(all.data?.needs_review ?? [])].map((r) => r.batch_id)
    return [...new Set([...uploaded, ...fromResults])]
  }, [all.data, uploaded])
  const batchQueries = useQueries({
    queries: batchIds.map((id) => ({
      queryKey: ['batch', opening.id, id],
      queryFn: () => getBatch(opening.id, id),
      refetchInterval: (query: { state: { data?: ScreeningBatch } }) => (query.state.data?.complete === false ? POLL_MS : false),
    })),
  })
  const batches = batchQueries
    .map((q) => q.data)
    .filter((b): b is ScreeningBatch => b !== undefined)
    .sort((a, b) => b.submitted_at.localeCompare(a.submitted_at))
  const processing = batches.some((b) => !b.complete) || (all.data?.pending ?? 0) > 0
  useNotifyWhenFinished(batches)

  // Results only poll while something is processing, so the poll that sees a batch finish would
  // otherwise leave the ranking one step behind. Refresh it whenever any batch's counts move.
  const queryClient = useQueryClient()
  const progress = batches.map((b) => `${b.id}:${b.counts.pending}`).join()
  useEffect(() => {
    if (progress) queryClient.invalidateQueries({ queryKey: ['results', opening.id] })
  }, [progress, opening.id, queryClient])

  // Against a Hiring Service that does not serve screening, say so once, plainly, rather than
  // showing an upload that cannot work and a red error under it.
  if (all.error instanceof ApiError && all.error.notServed) {
    return (
      <Alert variant="light" color="blue" icon={<IconInfoCircle size={16} />} title={t('screening.notServedTitle')}>
        {t('screening.notServed')}
      </Alert>
    )
  }

  const reasons = new Map((all.data?.needs_review ?? []).map((r) => [r.resume_id, r.reason]))
  return (
    <Stack>
      {opening.status === 'open' ? (
        <BatchUpload openingId={opening.id} onSubmitted={remember} />
      ) : (
        <Alert variant="light" color="gray">
          {t('opening.notOpenForScreening')}
        </Alert>
      )}
      {batches.length > 0 && <BatchList batches={batches} reasons={reasons} />}
      <ResultsTable
        opening={opening}
        live={processing}
        onOpen={(id) => setSearchParams((p) => ({ ...Object.fromEntries(p), resume: id }))}
      />
      <CandidateDrawer
        opening={opening}
        resumeId={resumeId}
        onClose={() =>
          setSearchParams((p) => {
            p.delete('resume')
            return p
          })
        }
      />
    </Stack>
  )
}

/**
 * The batches submitted from this browser, so one shows its progress before any of its resumes
 * has a result. Kept per opening in localStorage; without storage, a batch appears once its
 * first result arrives.
 */
function useUploadedBatches(openingId: string): [string[], (batchId: string) => void] {
  const key = `hireassist.batches.${openingId}`
  const [ids, setIds] = useState<string[]>(() => {
    try {
      return JSON.parse(localStorage.getItem(key) ?? '[]') as string[]
    } catch {
      return []
    }
  })
  const remember = useCallback(
    (batchId: string) =>
      setIds((current) => {
        const next = [batchId, ...current.filter((id) => id !== batchId)].slice(0, 50)
        try {
          localStorage.setItem(key, JSON.stringify(next))
        } catch {
          // Storage unavailable: the batch still appears once a result arrives.
        }
        return next
      }),
    [key],
  )
  return [ids, remember]
}

/** FR-2.12: tell the Recruiter when a batch they are watching finishes. */
function useNotifyWhenFinished(batches: ScreeningBatch[]) {
  const { t } = useI18n()
  const seen = useRef(new Map<string, boolean>())
  useEffect(() => {
    for (const b of batches) {
      if (seen.current.get(b.id) === false && b.complete) {
        notifications.show({
          color: b.counts.needs_review ? 'yellow' : 'green',
          message: t('batch.finished', { screened: b.counts.screened, review: b.counts.needs_review + b.counts.failed }),
        })
      }
      seen.current.set(b.id, b.complete)
    }
  }, [batches, t])
}

function BatchUpload({ openingId, onSubmitted }: { openingId: string; onSubmitted: (batchId: string) => void }) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const submit = useMutation({
    mutationFn: (files: File[]) => submitScreeningBatch(openingId, files),
    onSuccess: (batch) => {
      notifications.show({ color: 'indigo', message: t('batch.submitted', { count: batch.entries.length }) })
      for (const r of batch.rejected ?? []) {
        notifications.show({ color: 'orange', message: t('batch.leftOut', { file: r.file_name, reason: r.reason }) })
      }
      queryClient.setQueryData(['batch', openingId, batch.id], batch)
      onSubmitted(batch.id)
    },
    onError: (err) => notifications.show({ color: 'red', message: err.message }),
  })

  return (
    <Dropzone
      onDrop={(files) => submit.mutate(files)}
      onReject={() => notifications.show({ color: 'red', message: t('batch.rejected') })}
      accept={[MIME_TYPES.pdf]}
      maxFiles={100}
      loading={submit.isPending}
      aria-label={t('batch.uploadTitle')}
    >
      <Group justify="center" gap="lg" mih={110} style={{ pointerEvents: 'none' }}>
        <Dropzone.Accept>
          <IconUpload size={44} stroke={1.5} color="var(--mantine-color-indigo-6)" />
        </Dropzone.Accept>
        <Dropzone.Reject>
          <IconX size={44} stroke={1.5} color="var(--mantine-color-red-6)" />
        </Dropzone.Reject>
        <Dropzone.Idle>
          <IconFileTypePdf size={44} stroke={1.5} color="var(--mantine-color-dimmed)" />
        </Dropzone.Idle>
        <div>
          <Text size="lg" fw={600}>
            {t('batch.drop')}
          </Text>
          <Text size="sm" c="dimmed">
            {t('batch.dropHint')}
          </Text>
        </div>
      </Group>
    </Dropzone>
  )
}

function BatchList({ batches, reasons }: { batches: ScreeningBatch[]; reasons: Map<string, string | undefined> }) {
  const { t, formatDate } = useI18n()
  return (
    <Paper withBorder p="md">
      <Title order={5} mb="xs">
        {t('batch.batches')}
      </Title>
      <Stack gap="sm">
        {batches.map((b) => {
          const total = b.entries.length
          const done = total - b.counts.pending
          const review = b.entries.filter((e) => e.status === 'needs_review' || e.status === 'failed')
          return (
            <div key={b.id}>
              <Group justify="space-between" mb={4}>
                <Group gap="xs">
                  <Text size="sm" fw={500}>
                    {formatDate(b.submitted_at)}
                  </Text>
                  <Badge size="sm" variant="light" color={b.complete ? 'green' : 'indigo'}>
                    {b.complete ? t('batch.complete') : t('batch.processing')}
                  </Badge>
                </Group>
                <Text size="sm" c="dimmed">
                  {t('batch.progress', { done, total })}
                </Text>
              </Group>
              <Progress.Root size="md">
                <Progress.Section value={(b.counts.screened / total) * 100} color="green" />
                <Progress.Section value={((b.counts.needs_review + b.counts.failed) / total) * 100} color="orange" />
                <Progress.Section value={(b.counts.pending / total) * 100} color="gray.3" animated={!b.complete} striped />
              </Progress.Root>
              {review.length > 0 && (
                <Group gap={6} mt={6}>
                  <IconAlertTriangle size={14} color="var(--mantine-color-orange-6)" />
                  <Text size="xs" c="orange.8">
                    {t('batch.needsReview')}:
                  </Text>
                  {review.map((e) => (
                    <Tooltip key={e.resume_id} label={reasons.get(e.resume_id) ?? t('batch.failedNoReason')}>
                      <Badge size="xs" variant="outline" color="orange">
                        {e.file_name}
                      </Badge>
                    </Tooltip>
                  ))}
                </Group>
              )}
            </div>
          )
        })}
      </Stack>
    </Paper>
  )
}
