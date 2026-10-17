import { Alert, Badge, Button, Divider, Drawer, Group, NumberInput, Paper, Stack, Text, Textarea, ThemeIcon } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconCheck, IconLock, IconMessageQuestion, IconStar, IconX } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { getCandidate, overrideScore, shortlistCandidate } from '../../api/endpoints'
import type { CandidateDetail, JobOpening } from '../../api/types'
import { useI18n } from '../../i18n/I18nProvider'
import { DecisionBadge, PageLoader, QueryError } from '../common'
import { scoreColor } from '../rules'

/**
 * One candidate against one opening: the justification first, because a score a recruiter
 * cannot explain is the failure this product exists to prevent (NFR-15). Then override,
 * shortlist, and the way into UC-3.
 */
export function CandidateDrawer({ opening, candidateId, onClose }: { opening: JobOpening; candidateId: string | null; onClose: () => void }) {
  const { t } = useI18n()
  const candidate = useQuery({
    queryKey: ['candidate', opening.id, candidateId],
    queryFn: () => getCandidate(opening.id, candidateId!),
    enabled: candidateId !== null,
  })

  return (
    <Drawer opened={candidateId !== null} onClose={onClose} position="right" size="xl" title={t('candidate.title')}>
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

function CandidateBody({ opening, c }: { opening: JobOpening; c: CandidateDetail }) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['candidate', opening.id, c.candidate_id] })
    queryClient.invalidateQueries({ queryKey: ['results', opening.id] })
  }
  const shortlist = useMutation({
    mutationFn: () => shortlistCandidate(opening.id, c.candidate_id),
    onSuccess: refresh,
    onError: (err) => notifications.show({ color: 'red', message: err.message }),
  })
  const guidePath = `/job-openings/${opening.id}/candidates/${c.candidate_id}/interview-guide`

  return (
    <Stack>
      <Group justify="space-between" align="flex-start">
        <div>
          <Text fw={600} size="lg">
            {c.file_name}
          </Text>
          <DecisionBadge decision={c.decision} />
        </div>
        <Stack gap={2} align="flex-end">
          <Badge color={scoreColor(c.score)} size="xl" radius="sm">
            {c.score}
          </Badge>
          {c.override && (
            <Text size="xs" c="dimmed">
              {t('candidate.aiScore')}: {c.ai_score}
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
        <Text size="sm">{c.justification}</Text>
        {c.override && (
          <Text size="sm" mt="xs" c="indigo.7">
            {t('candidate.yourScore')}: {c.override.score} — {c.override.reason}
          </Text>
        )}
      </Paper>

      <div>
        <Text fw={600} size="sm" mb="xs">
          {t('candidate.assessments')}
        </Text>
        <Stack gap="xs">
          {c.assessments.map((a) => {
            const criterion = opening.criteria.find((x) => x.name === a.criterion)
            return (
              <Group key={a.criterion} gap="sm" align="flex-start" wrap="nowrap">
                <ThemeIcon size="sm" radius="xl" color={a.met ? 'green' : 'red'} variant="light" mt={2}>
                  {a.met ? <IconCheck size={12} /> : <IconX size={12} />}
                </ThemeIcon>
                <div style={{ flex: 1 }}>
                  <Group gap={6}>
                    <Text size="sm" fw={500}>
                      {a.criterion}
                    </Text>
                    {criterion?.classification === 'must-have' && (
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
            )
          })}
        </Stack>
      </div>

      <Divider />

      <Group>
        {c.decision === 'undecided' && (
          <Button leftSection={<IconStar size={16} />} color="green" loading={shortlist.isPending} onClick={() => shortlist.mutate()}>
            {t('candidate.shortlist')}
          </Button>
        )}
        {c.decision === 'shortlisted' && (
          <Button
            component={Link}
            to={guidePath}
            leftSection={<IconMessageQuestion size={16} />}
            variant={c.has_interview_guide ? 'light' : 'filled'}
          >
            {c.has_interview_guide ? t('candidate.openGuide') : t('candidate.generateGuide')}
          </Button>
        )}
      </Group>
      {c.decision === 'undecided' && (
        <Text size="xs" c="dimmed">
          {t('candidate.guideNeedsShortlist')}
        </Text>
      )}

      <OverrideForm opening={opening} c={c} onDone={refresh} />

      <Divider />
      <Profile c={c} />
    </Stack>
  )
}

/** FR-2.8: the Recruiter's score replaces the AI's in the ranking; the AI's is kept. */
function OverrideForm({ opening, c, onDone }: { opening: JobOpening; c: CandidateDetail; onDone: () => void }) {
  const { t } = useI18n()
  const [score, setScore] = useState<number | ''>(c.override?.score ?? '')
  const [reason, setReason] = useState(c.override?.reason ?? '')
  const save = useMutation({
    mutationFn: () => overrideScore(opening.id, c.candidate_id, Number(score), reason),
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
        <Textarea label={t('candidate.overrideReason')} autosize minRows={1} value={reason} onChange={(e) => setReason(e.currentTarget.value)} style={{ flex: 1 }} />
        <Button variant="light" disabled={score === '' || !reason.trim()} loading={save.isPending} onClick={() => save.mutate()}>
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

function Profile({ c }: { c: CandidateDetail }) {
  const { t } = useI18n()
  return (
    <Stack gap="sm">
      <Text fw={600} size="sm">
        {t('candidate.profile')}
      </Text>
      <div>
        <Text size="xs" c="dimmed" mb={4}>
          {t('candidate.skills')}
        </Text>
        <Group gap={4}>
          {c.profile.skills.map((s) => (
            <Badge key={s} variant="default" size="sm">
              {s}
            </Badge>
          ))}
        </Group>
      </div>
      <div>
        <Text size="xs" c="dimmed" mb={4}>
          {t('candidate.experience')}
        </Text>
        {c.profile.work_history.map((w, i) => (
          <Text key={i} size="sm">
            <b>{w.title}</b>, {w.organisation} ({w.start}–{w.end || t('candidate.present')}) — {w.summary}
          </Text>
        ))}
      </div>
      <div>
        <Text size="xs" c="dimmed" mb={4}>
          {t('candidate.education')}
        </Text>
        {c.profile.education.map((e, i) => (
          <Text key={i} size="sm">
            {e.qualification}, {e.institution} ({e.completed})
          </Text>
        ))}
      </div>
    </Stack>
  )
}
