import { Alert, Button, Group, Modal, Paper, SegmentedControl, Stack, Tabs, Text, Title } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconAlertTriangle, IconArrowLeft, IconPlayerPause, IconPlayerPlay, IconX } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { closeOpening, getJobOpening, pauseOpening, resumeOpening, reviseScreeningCriteria } from '../api/endpoints'
import type { Criterion, JobOpening, Outcome } from '../api/types'
import { PageLoader, QueryError, StatusBadge } from '../components/common'
import { CriteriaEditor, CriteriaList } from '../components/CriteriaEditor'
import { criteriaValid } from '../components/rules'
import { ScreeningTab } from '../components/screening/ScreeningTab'
import { useI18n } from '../i18n/I18nProvider'

export function JobOpeningPage() {
  const { id = '' } = useParams()
  const { t, formatDate } = useI18n()
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = searchParams.get('tab') ?? 'screening'

  const opening = useQuery({ queryKey: ['opening', id], queryFn: () => getJobOpening(id) })

  if (opening.isPending) return <PageLoader />
  if (opening.isError) return <QueryError error={opening.error} onRetry={() => opening.refetch()} />
  const o = opening.data

  return (
    <Stack>
      <Button component={Link} to="/job-openings" variant="subtle" size="xs" leftSection={<IconArrowLeft size={14} />} w="fit-content" px={4}>
        {t('nav.jobOpenings')}
      </Button>
      <Group justify="space-between" align="flex-start">
        <div>
          <Group gap="sm">
            <Title order={2}>{o.title}</Title>
            <StatusBadge opening={o} />
          </Group>
          <Text size="sm" c="dimmed">
            {t('opening.openedOn', { date: formatDate(o.opened_at) })} · {t('common.days', { count: o.expected_time_to_fill_days })}
          </Text>
        </div>
        <LifecycleActions opening={o} />
      </Group>

      <Tabs value={tab} onChange={(value) => setSearchParams(value === 'screening' ? {} : { tab: value ?? '' })}>
        <Tabs.List>
          <Tabs.Tab value="screening">{t('opening.tab.screening')}</Tabs.Tab>
          <Tabs.Tab value="criteria">{t('opening.tab.criteria')}</Tabs.Tab>
          <Tabs.Tab value="description">{t('opening.tab.description')}</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="screening" pt="md">
          <ScreeningTab opening={o} />
        </Tabs.Panel>
        <Tabs.Panel value="criteria" pt="md">
          <CriteriaPanel opening={o} />
        </Tabs.Panel>
        <Tabs.Panel value="description" pt="md">
          <Paper withBorder p="lg">
            <Text style={{ whiteSpace: 'pre-wrap' }}>{o.description || '—'}</Text>
          </Paper>
        </Tabs.Panel>
      </Tabs>
    </Stack>
  )
}

function useOpeningMutation<A>(id: string, fn: (arg: A) => Promise<JobOpening>) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: (updated) => {
      queryClient.setQueryData(['opening', id], updated)
      queryClient.invalidateQueries({ queryKey: ['openings'] })
    },
    onError: (err) => notifications.show({ color: 'red', message: err.message }),
  })
}

/** Pause, resume and close (FR-1.8). Closing is final (FR-1.9), so it asks first. */
function LifecycleActions({ opening }: { opening: JobOpening }) {
  const { t } = useI18n()
  const [closing, setClosing] = useState(false)
  const [outcome, setOutcome] = useState<Outcome>('filled')
  const pause = useOpeningMutation(opening.id, () => pauseOpening(opening.id))
  const resume = useOpeningMutation(opening.id, () => resumeOpening(opening.id))
  const close = useOpeningMutation(opening.id, (o: Outcome) => closeOpening(opening.id, o))

  if (opening.status === 'closed') return null
  return (
    <Group gap="xs">
      {opening.status === 'open' ? (
        <Button variant="default" leftSection={<IconPlayerPause size={16} />} loading={pause.isPending} onClick={() => pause.mutate(undefined)}>
          {t('opening.pause')}
        </Button>
      ) : (
        <Button variant="default" leftSection={<IconPlayerPlay size={16} />} loading={resume.isPending} onClick={() => resume.mutate(undefined)}>
          {t('opening.resume')}
        </Button>
      )}
      <Button variant="light" color="red" leftSection={<IconX size={16} />} onClick={() => setClosing(true)}>
        {t('opening.close')}
      </Button>
      <Modal opened={closing} onClose={() => setClosing(false)} title={t('opening.closeTitle')} centered>
        <Stack>
          <Text size="sm">{t('opening.closeHint')}</Text>
          <SegmentedControl
            value={outcome}
            onChange={(v) => setOutcome(v as Outcome)}
            data={[
              { value: 'filled', label: t('outcome.filled') },
              { value: 'cancelled', label: t('outcome.cancelled') },
            ]}
          />
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setClosing(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              color="red"
              loading={close.isPending}
              onClick={() => close.mutate(outcome, { onSuccess: () => setClosing(false) })}
            >
              {t('opening.closeConfirm')}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Group>
  )
}

/** Shows the criteria, and lets an open or paused opening's criteria be revised. */
function CriteriaPanel({ opening }: { opening: JobOpening }) {
  const { t } = useI18n()
  const [draft, setDraft] = useState<Criterion[] | null>(null)
  const revise = useOpeningMutation(opening.id, (criteria: Criterion[]) => reviseScreeningCriteria(opening.id, criteria))

  if (draft) {
    return (
      <Paper withBorder p="lg">
        <Stack>
          <Alert variant="light" icon={<IconAlertTriangle size={16} />}>
            {t('criteria.snapshotHint')}
          </Alert>
          <CriteriaEditor value={draft} onChange={setDraft} />
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setDraft(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              disabled={!criteriaValid(draft)}
              loading={revise.isPending}
              onClick={() => revise.mutate(draft, { onSuccess: () => setDraft(null) })}
            >
              {t('common.save')}
            </Button>
          </Group>
        </Stack>
      </Paper>
    )
  }

  return (
    <Paper withBorder p="lg">
      <Stack>
        <Group justify="space-between">
          <Title order={4}>{t('criteria.title')}</Title>
          {opening.status !== 'closed' && (
            <Button variant="light" size="xs" onClick={() => setDraft(opening.criteria)}>
              {t('criteria.edit')}
            </Button>
          )}
        </Group>
        <CriteriaList criteria={opening.criteria} />
        <Text size="xs" c="dimmed">
          {opening.status === 'closed' ? t('criteria.closedHint') : t('criteria.mustHaveHint')}
        </Text>
      </Stack>
    </Paper>
  )
}
