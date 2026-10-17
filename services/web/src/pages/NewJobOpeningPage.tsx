import { Alert, Button, Group, List, NumberInput, Paper, Stack, Stepper, Text, Textarea, TextInput, Title } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconAlertTriangle, IconSparkles } from '@tabler/icons-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { createJobOpening, proposeJobOpening } from '../api/endpoints'
import type { Criterion } from '../api/types'
import { CriteriaEditor } from '../components/CriteriaEditor'
import { criteriaValid } from '../components/rules'
import { useI18n } from '../i18n/I18nProvider'

/**
 * UC-1. Two steps: describe the role in plain language and get a proposal, then review and
 * correct it. Nothing is stored until the Recruiter confirms (FR-1.3) — the review step is
 * where a human corrects the model, before any resume is scored against a wrong criterion.
 */
export function NewJobOpeningPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [step, setStep] = useState(0)
  const [description, setDescription] = useState('')
  const [title, setTitle] = useState('')
  const [criteria, setCriteria] = useState<Criterion[]>([])
  const [uninterpreted, setUninterpreted] = useState<string[]>([])
  const [timeToFill, setTimeToFill] = useState<number>(30)
  const [proposalFailed, setProposalFailed] = useState(false)

  const propose = useMutation({
    mutationFn: () => proposeJobOpening(description),
    onSuccess: (proposal) => {
      setTitle(proposal.proposed_title)
      setCriteria(proposal.criteria)
      setUninterpreted(proposal.uninterpreted)
      setProposalFailed(false)
      setStep(1)
    },
    // FR-1.5 / flow 4a: the AI being down must not stop a Recruiter creating an opening.
    onError: () => setProposalFailed(true),
  })

  const create = useMutation({
    mutationFn: () =>
      createJobOpening({ title, description, criteria, expected_time_to_fill_days: timeToFill }),
    onSuccess: (opening) => {
      queryClient.invalidateQueries({ queryKey: ['openings'] })
      notifications.show({ color: 'green', message: opening.title })
      navigate(`/job-openings/${opening.id}`)
    },
  })

  const enterManually = () => {
    setTitle('')
    setCriteria([{ name: '', classification: 'must-have', weight: 5 }])
    setUninterpreted([])
    setStep(1)
  }

  const canCreate = title.trim() && criteriaValid(criteria) && timeToFill > 0

  return (
    <Stack>
      <Title order={2}>{t('create.title')}</Title>
      <Stepper active={step} onStepClick={(s) => s < step && setStep(s)} size="sm">
        <Stepper.Step label={t('create.step.describe')}>
          <Paper withBorder p="lg" mt="md">
            <Stack>
              <Textarea
                label={t('create.description')}
                description={t('create.descriptionHint')}
                placeholder={t('create.descriptionPlaceholder')}
                autosize
                minRows={6}
                value={description}
                onChange={(e) => setDescription(e.currentTarget.value)}
              />
              {proposalFailed && (
                <Alert color="orange" icon={<IconAlertTriangle size={16} />}>
                  {t('create.aiUnavailable')}
                </Alert>
              )}
              <Group justify="space-between">
                <Button variant="subtle" onClick={enterManually}>
                  {t('create.manual')}
                </Button>
                <Button
                  leftSection={<IconSparkles size={16} />}
                  disabled={!description.trim()}
                  loading={propose.isPending}
                  onClick={() => propose.mutate()}
                >
                  {t('create.propose')}
                </Button>
              </Group>
            </Stack>
          </Paper>
        </Stepper.Step>

        <Stepper.Step label={t('create.step.review')}>
          <Paper withBorder p="lg" mt="md">
            <Stack>
              <Text size="sm" c="dimmed">
                {t('create.reviewHint')}
              </Text>
              <Group grow align="flex-start">
                <TextInput
                  label={t('create.jobTitle')}
                  required
                  value={title}
                  onChange={(e) => setTitle(e.currentTarget.value)}
                />
                <NumberInput
                  label={t('create.timeToFill')}
                  required
                  min={1}
                  allowDecimal={false}
                  value={timeToFill}
                  onChange={(v) => setTimeToFill(typeof v === 'number' ? v : 0)}
                  maw={260}
                />
              </Group>
              {uninterpreted.length > 0 && (
                <Alert color="yellow" icon={<IconAlertTriangle size={16} />} title={t('create.uninterpreted')}>
                  <List size="sm">
                    {uninterpreted.map((u) => (
                      <List.Item key={u}>{u}</List.Item>
                    ))}
                  </List>
                </Alert>
              )}
              <CriteriaEditor value={criteria} onChange={setCriteria} />
              {create.isError && (
                <Alert color="red" icon={<IconAlertTriangle size={16} />}>
                  {create.error.message}
                </Alert>
              )}
              <Group justify="space-between">
                <Button variant="default" onClick={() => setStep(0)}>
                  {t('common.back')}
                </Button>
                <Button disabled={!canCreate} loading={create.isPending} onClick={() => create.mutate()}>
                  {t('create.submit')}
                </Button>
              </Group>
            </Stack>
          </Paper>
        </Stepper.Step>
      </Stepper>
    </Stack>
  )
}
