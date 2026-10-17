import { Badge, Group, Loader, NumberInput, Paper, Select, Table, Text, Title, Tooltip } from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { getRankedResults } from '../../api/endpoints'
import type { JobOpening } from '../../api/types'
import { useI18n } from '../../i18n/I18nProvider'
import { DecisionBadge, PageLoader, QueryError } from '../common'
import { scoreColor } from '../rules'
import { POLL_MS } from './poll'

/**
 * The ranked list (FR-2.6): highest score first, re-ranked as results arrive, filterable by
 * minimum score and by a criterion the candidate meets. Resumes needing review never appear
 * here (FR-2.7) — they are listed under their batch instead.
 */
export function ResultsTable({ opening, live, onOpen }: { opening: JobOpening; live: boolean; onOpen: (candidateId: string) => void }) {
  const { t } = useI18n()
  const [minScore, setMinScore] = useState<number>(0)
  const [criterion, setCriterion] = useState<string | null>(null)
  const [debouncedMin] = useDebouncedValue(minScore, 300)

  const results = useQuery({
    queryKey: ['results', opening.id, debouncedMin, criterion],
    queryFn: () => getRankedResults(opening.id, { minScore: debouncedMin, criterion: criterion ?? undefined }),
    refetchInterval: live ? POLL_MS : false,
    placeholderData: (previous) => previous,
  })
  const filtered = debouncedMin > 0 || criterion !== null

  return (
    <Paper withBorder p="md">
      <Group justify="space-between" mb="sm" align="flex-end">
        <Group gap="xs">
          <Title order={4}>{t('results.title')}</Title>
          {live && <Loader size="xs" type="dots" />}
        </Group>
        <Group gap="sm" align="flex-end">
          <NumberInput
            label={t('results.minScore')}
            size="xs"
            w={130}
            min={0}
            max={100}
            allowDecimal={false}
            value={minScore}
            onChange={(v) => setMinScore(typeof v === 'number' ? v : 0)}
          />
          <Select
            label={t('results.criterion')}
            size="xs"
            w={260}
            clearable
            placeholder={t('results.anyCriterion')}
            data={opening.criteria.map((c) => c.name)}
            value={criterion}
            onChange={setCriterion}
          />
        </Group>
      </Group>

      {results.isPending ? (
        <PageLoader />
      ) : results.isError ? (
        <QueryError error={results.error} onRetry={() => results.refetch()} />
      ) : results.data.length === 0 ? (
        <Text c="dimmed" ta="center" py="lg">
          {filtered ? t('results.noMatch') : t('results.empty')}
        </Text>
      ) : (
        <Table highlightOnHover verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th w={40}>{t('results.col.rank')}</Table.Th>
              <Table.Th>{t('results.col.resume')}</Table.Th>
              <Table.Th w={140}>{t('results.col.score')}</Table.Th>
              <Table.Th>{t('results.col.mustHaves')}</Table.Th>
              <Table.Th w={150}>{t('results.col.decision')}</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {results.data.map((r, i) => (
              <Table.Tr key={r.candidate_id} style={{ cursor: 'pointer' }} onClick={() => onOpen(r.candidate_id)} data-testid="result-row">
                <Table.Td c="dimmed">{i + 1}</Table.Td>
                <Table.Td fw={500}>{r.file_name}</Table.Td>
                <Table.Td>
                  <Group gap={6}>
                    <Badge color={scoreColor(r.score)} variant="filled" size="lg" radius="sm" w={52}>
                      {r.score}
                    </Badge>
                    {r.override && (
                      <Tooltip label={r.override.reason}>
                        <Text size="xs" c="dimmed">
                          {t('results.overridden', { score: r.ai_score })}
                        </Text>
                      </Tooltip>
                    )}
                  </Group>
                </Table.Td>
                <Table.Td>
                  {r.meets_must_haves ? (
                    <Text size="sm" c="green.7">
                      {t('results.allMet')}
                    </Text>
                  ) : (
                    <Tooltip label={r.unmet_must_haves.join(', ')}>
                      <Text size="sm" c="red.7">
                        {t('results.missing', { count: r.unmet_must_haves.length })}
                      </Text>
                    </Tooltip>
                  )}
                </Table.Td>
                <Table.Td>
                  <DecisionBadge decision={r.decision} />
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </Paper>
  )
}
