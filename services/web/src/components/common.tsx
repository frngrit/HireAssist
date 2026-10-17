import { Alert, Badge, Button, Center, Loader } from '@mantine/core'
import { IconAlertTriangle } from '@tabler/icons-react'
import type { DecisionState, JobOpening } from '../api/types'
import { useI18n } from '../i18n/I18nProvider'

const STATUS_COLOR = { open: 'green', paused: 'yellow', closed: 'gray' } as const

export function StatusBadge({ opening }: { opening: Pick<JobOpening, 'status' | 'outcome'> }) {
  const { t } = useI18n()
  return (
    <Badge color={STATUS_COLOR[opening.status]} variant="light">
      {t(`status.${opening.status}`)}
      {opening.outcome ? ` · ${t(`outcome.${opening.outcome}`)}` : ''}
    </Badge>
  )
}

const DECISION_COLOR = { shortlisted: 'green', undecided: 'gray', not_qualified: 'red' } as const

export function DecisionBadge({ decision }: { decision: DecisionState }) {
  const { t } = useI18n()
  return (
    <Badge color={DECISION_COLOR[decision]} variant={decision === 'undecided' ? 'outline' : 'light'}>
      {t(`decision.${decision}`)}
    </Badge>
  )
}

export function PageLoader() {
  return (
    <Center py="xl">
      <Loader />
    </Center>
  )
}

export function QueryError({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const { t } = useI18n()
  return (
    <Alert color="red" icon={<IconAlertTriangle size={16} />} title={t('common.error')}>
      {error instanceof Error ? error.message : String(error)}
      {onRetry && (
        <Button size="xs" variant="light" color="red" mt="sm" display="block" onClick={onRetry}>
          {t('common.retry')}
        </Button>
      )}
    </Alert>
  )
}
