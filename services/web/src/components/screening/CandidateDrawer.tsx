import { Alert, Badge, Button, Divider, Drawer, Group, NumberInput, Paper, Stack, Text, ThemeIcon } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconCheck, IconLock, IconMessageQuestion, IconStar, IconX } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { getScreeningResult, overrideScore, shortlistCandidate } from '../../api/endpoints'
import { decisionOf, type JobOpening, type ScreenedCandidate, type ScreeningResult } from '../../api/types'
import { useI18n } from '../../i18n/I18nProvider'
import { DecisionBadge, PageLoader, QueryError } from '../common'
import { scoreColor } from '../rules'

/**
 * One screened resume against one opening: the justification first, because a score a recruiter
 * cannot explain is the failure this product exists to prevent (NFR-15). Then override,
 * shortlist, and the way into UC-3.
 */
export function CandidateDrawer({ opening, resumeId, onClose }: { opening: JobOpening; resumeId: string | null; onClose: () => void }) {
  const { t } = useI18n()
  const candidate = useQuery({
    queryKey: ['candidate', opening.id, resumeId],
    queryFn: () => getScreeningResult(opening.id, resumeId!),
    enabled: resumeId !== null,
  })

  return (
    <Drawer opened={resumeId !== null} onClose={onClose} position="right" size="xl" title={t('candidate.title')}>
      {candidate.isPending ? (
        <PageLoader />
      ) : candidate.isError ? (
        <QueryError error={candidate.error} />
      ) : (
        <CandidateBody opening={opening} c={candidate.data} />
      )}
    </Drawer>
  )
}

function CandidateBody({ opening, c }: { opening: JobOpening; c: ScreenedCandidate }) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const r = c.result
  const decision = decisionOf(r)
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['candidate', opening.id, r.resume_id] })
    queryClient.invalidateQueries({ queryKey: ['results', opening.id] })
  }
  const shortlist = useMutation({
    mutationFn: () => shortlistCandidate(opening.id, r.resume_id),
    onSuccess: refresh,
    onError: (err) => notifications.show({ color: 'red', message: err.message }),
  })

  return (
    <Stack>
      <Group justify="space-between" align="flex-start">
        <div>
          <Text fw={600} size="lg">
            {r.file_name}
          </Text>
          <DecisionBadge decision={decision} />
        </div>
        <Stack gap={2} align="flex-end">
          <Badge color={scoreColor(r.score ?? 0)} size="xl" radius="sm">
            {r.score}
          </Badge>
          {r.original_score !== undefined && (
            <Text size="xs" c="dimmed">
              {t('candidate.aiScore')}: {r.original_score}
            </Text>
          )}
        </Stack>
      </Group>

      <Paper withBorder p="md" bg="var(--mantine-color-default-hover)">
        <Group justify="space-between" mb={4}>
          <Text fw={600} size="sm">
            {t('candidate.justification')}
          </Text>
          <Group gap={4}>
            <IconLock size={12} color="var(--mantine-color-dimmed)" />
            <Text size="xs" c="dimmed">
              {t('candidate.internalOnly')}
            </Text>
          </Group>
        </Group>
        {c.justification ? (
          <Text size="sm">{c.justification.text}</Text>
        ) : (
          <Text size="sm" c="dimmed">
            {t('candidate.justificationUnavailable')}
          </Text>
        )}
      </Paper>

      {c.justification && c.justification.evidence.length > 0 && (
        <div>
          <Text fw={600} size="sm" mb="xs">
            {t('candidate.assessments')}
          </Text>
          <Stack gap="xs">
            {c.justification.evidence.map((a) => (
              <Group key={a.criterion} gap="sm" align="flex-start" wrap="nowrap">
                <ThemeIcon size="sm" radius="xl" color={a.met ? 'green' : 'red'} variant="light" mt={2}>
                  {a.met ? <IconCheck size={12} /> : <IconX size={12} />}
                </ThemeIcon>
                <div style={{ flex: 1 }}>
                  <Group gap={6}>
                    <Text size="sm" fw={500}>
                      {a.criterion}
                    </Text>
                    {a.classification === 'must-have' && (
                      <Badge size="xs" color="red" variant="outline">
                        {t('classification.must-have')}
                      </Badge>
                    )}
                    <Text size="xs" c="dimmed">
                      {a.score}/100
                    </Text>
                  </Group>
                  <Text size="xs" c="dimmed" fs="italic">
                    {t('candidate.evidence')}: “{a.evidence}”
                  </Text>
                </div>
              </Group>
            ))}
          </Stack>
        </div>
      )}

      <Divider />

      <Group>
        {decision === 'undecided' && (
          <Button leftSection={<IconStar size={16} />} color="green" loading={shortlist.isPending} onClick={() => shortlist.mutate()}>
            {t('candidate.shortlist')}
          </Button>
        )}
        {decision === 'shortlisted' && r.candidate_id && (
          <Button
            component={Link}
            to={`/job-openings/${opening.id}/candidates/${r.candidate_id}/interview-guide`}
            leftSection={<IconMessageQuestion size={16} />}
          >
            {t('candidate.guide')}
          </Button>
        )}
      </Group>
      {decision === 'undecided' && (
        <Text size="xs" c="dimmed">
          {t('candidate.guideNeedsShortlist')}
        </Text>
      )}

      <OverrideForm opening={opening} r={r} onDone={refresh} />
    </Stack>
  )
}

/** FR-2.8: the Recruiter's score replaces screening's in the ranking; screening's is kept. */
function OverrideForm({ opening, r, onDone }: { opening: JobOpening; r: ScreeningResult; onDone: () => void }) {
  const { t } = useI18n()
  const [score, setScore] = useState<number | ''>(r.original_score !== undefined ? (r.score ?? '') : '')
  const save = useMutation({
    mutationFn: () => overrideScore(opening.id, r.resume_id, Number(score)),
    onSuccess: onDone,
  })
  return (
    <Paper withBorder p="md">
      <Text fw={600} size="sm">
        {t('candidate.override')}
      </Text>
      <Text size="xs" c="dimmed" mb="xs">
        {t('candidate.overrideHint')}
      </Text>
      <Group align="flex-end" wrap="nowrap">
        <NumberInput
          label={t('candidate.overrideScore')}
          min={0}
          max={100}
          allowDecimal={false}
          value={score}
          onChange={(v) => setScore(typeof v === 'number' ? v : '')}
          w={160}
        />
        <Button variant="light" disabled={score === ''} loading={save.isPending} onClick={() => save.mutate()}>
          {t('common.save')}
        </Button>
      </Group>
      {save.isError && (
        <Alert color="red" mt="xs" py={6}>
          {save.error.message}
        </Alert>
      )}
    </Paper>
  )
}
