import { Alert, Badge, Group, Paper, Progress, Stack, Text, Title, Tooltip } from '@mantine/core'
import { Dropzone, MIME_TYPES } from '@mantine/dropzone'
import { notifications } from '@mantine/notifications'
import { IconAlertTriangle, IconFileTypePdf, IconInfoCircle, IconUpload, IconX } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ApiError } from '../../api/client'
import { listBatches, submitScreeningBatch } from '../../api/endpoints'
import type { JobOpening, ScreeningBatch } from '../../api/types'
import { useI18n } from '../../i18n/I18nProvider'
import { CandidateDrawer } from './CandidateDrawer'
import { POLL_MS } from './poll'
import { ResultsTable } from './ResultsTable'

/** UC-2: submit a batch, watch it progress, and work the ranked list as results arrive. */
export function ScreeningTab({ opening }: { opening: JobOpening }) {
  const { t } = useI18n()
  const [searchParams, setSearchParams] = useSearchParams()
  const candidateId = searchParams.get('candidate')

  const batches = useQuery({
    queryKey: ['batches', opening.id],
    queryFn: () => listBatches(opening.id),
    retry: (count, error) => !(error instanceof ApiError && error.notServed) && count < 3,
    refetchInterval: (query) => (query.state.data?.some((b) => b.status === 'processing') ? POLL_MS : false),
  })
  const processing = batches.data?.some((b) => b.status === 'processing') ?? false
  useNotifyWhenFinished(batches.data)

  // Results only poll while something is processing, so the poll that sees a batch finish would
  // otherwise leave the ranking one step behind. Refresh it whenever any batch's counts move.
  const queryClient = useQueryClient()
  const progress = batches.data?.map((b) => `${b.id}:${b.counts.screened}`).join() ?? ''
  useEffect(() => {
    if (progress) queryClient.invalidateQueries({ queryKey: ['results', opening.id] })
  }, [progress, opening.id, queryClient])

  // Against a Hiring Service that does not serve screening yet, say so once, plainly, rather
  // than showing an upload that cannot work and a red error under it.
  if (batches.error instanceof ApiError && batches.error.notServed) {
    return (
      <Alert variant="light" color="blue" icon={<IconInfoCircle size={16} />} title={t('screening.notServedTitle')}>
        {t('screening.notServed')}
      </Alert>
    )
  }

  return (
    <Stack>
      {opening.status === 'open' ? (
        <BatchUpload openingId={opening.id} />
      ) : (
        <Alert variant="light" color="gray">
          {t('opening.notOpenForScreening')}
        </Alert>
      )}
      {batches.data && batches.data.length > 0 && <BatchList batches={batches.data} />}
      <ResultsTable
        opening={opening}
        live={processing}
        onOpen={(id) => setSearchParams((p) => ({ ...Object.fromEntries(p), candidate: id }))}
      />
      <CandidateDrawer
        opening={opening}
        candidateId={candidateId}
        onClose={() =>
          setSearchParams((p) => {
            p.delete('candidate')
            return p
          })
        }
      />
    </Stack>
  )
}

/** FR-2.12: tell the Recruiter when a batch they are watching finishes. */
function useNotifyWhenFinished(batches: ScreeningBatch[] | undefined) {
  const { t } = useI18n()
  const seen = useRef(new Map<string, ScreeningBatch['status']>())
  useEffect(() => {
    for (const b of batches ?? []) {
      const before = seen.current.get(b.id)
      if (before === 'processing' && b.status === 'finished') {
        notifications.show({
          color: b.counts.needs_review ? 'yellow' : 'green',
          message: t('batch.finished', { screened: b.counts.screened, review: b.counts.needs_review + b.counts.failed }),
        })
      }
      seen.current.set(b.id, b.status)
    }
  }, [batches, t])
}

function BatchUpload({ openingId }: { openingId: string }) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const submit = useMutation({
    mutationFn: (files: File[]) => submitScreeningBatch(openingId, files),
    onSuccess: (batch) => {
      notifications.show({ color: 'indigo', message: t('batch.submitted', { count: batch.total }) })
      queryClient.invalidateQueries({ queryKey: ['batches', openingId] })
    },
    onError: (err) => notifications.show({ color: 'red', message: err.message }),
  })

  return (
    <Dropzone
      onDrop={(files) => submit.mutate(files)}
      onReject={() => notifications.show({ color: 'red', message: t('batch.rejected') })}
      accept={[MIME_TYPES.pdf]}
      maxFiles={200}
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

function BatchList({ batches }: { batches: ScreeningBatch[] }) {
  const { t, formatDate } = useI18n()
  return (
    <Paper withBorder p="md">
      <Title order={5} mb="xs">
        {t('batch.batches')}
      </Title>
      <Stack gap="sm">
        {batches.map((b) => {
          const done = b.total - b.counts.pending
          const review = b.entries.filter((e) => e.status === 'needs_review' || e.status === 'failed')
          return (
            <div key={b.id}>
              <Group justify="space-between" mb={4}>
                <Group gap="xs">
                  <Text size="sm" fw={500}>
                    {formatDate(b.submitted_at)}
                  </Text>
                  <Badge size="sm" variant="light" color={b.status === 'finished' ? 'green' : 'indigo'}>
                    {b.status === 'finished' ? t('batch.complete') : t('batch.processing')}
                  </Badge>
                </Group>
                <Text size="sm" c="dimmed">
                  {t('batch.progress', { done, total: b.total })}
                </Text>
              </Group>
              <Progress.Root size="md">
                <Progress.Section value={(b.counts.screened / b.total) * 100} color="green" />
                <Progress.Section value={((b.counts.needs_review + b.counts.failed) / b.total) * 100} color="orange" />
                <Progress.Section value={(b.counts.pending / b.total) * 100} color="gray.3" animated={b.status === 'processing'} striped />
              </Progress.Root>
              {review.length > 0 && (
                <Group gap={6} mt={6}>
                  <IconAlertTriangle size={14} color="var(--mantine-color-orange-6)" />
                  <Text size="xs" c="orange.8">
                    {t('batch.needsReview')}:
                  </Text>
                  {review.map((e) => (
                    <Tooltip key={e.resume_id} label={e.reason}>
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
