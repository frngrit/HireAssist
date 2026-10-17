import { ActionIcon, Alert, Badge, Button, Group, Modal, Paper, Select, Stack, Text, Textarea, TextInput, Title, Tooltip } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconArrowLeft, IconPencil, IconPlus, IconRefresh, IconSparkles, IconTrash } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError } from '../api/client'
import {
  generateInterviewGuide,
  getCandidate,
  getInterviewGuide,
  getJobOpening,
  recordInterviewNote,
  regenerateInterviewGuide,
  reviseGuideQuestions,
} from '../api/endpoints'
import type { InterviewGuide, InterviewQuestion, JobOpening, QuestionDraft, QuestionKind } from '../api/types'
import { PageLoader, QueryError } from '../components/common'
import { useI18n } from '../i18n/I18nProvider'

const KIND_COLOR: Record<QuestionKind, string> = { 'resume-claim': 'blue', 'must-have-gap': 'red', 'role-depth': 'violet' }
const KINDS = Object.keys(KIND_COLOR) as QuestionKind[]

/** What the Recruiter sends back: the guide assigns ids, and notes are recorded on their own. */
const toDraft = ({ text, criterion, evidence, kind }: QuestionDraft): QuestionDraft => ({ text, criterion, evidence, kind })

/**
 * UC-3. Questions are generated from the candidate's profile and the opening's criteria, each
 * tagged with the criterion it tests and the evidence behind it (FR-3.3). The Recruiter edits
 * them, regenerates with an emphasis (FR-3.4), and takes notes during the interview (FR-3.5).
 */
export function InterviewGuidePage() {
  const { id = '', candidateId = '' } = useParams()
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const key = ['guide', id, candidateId]

  const opening = useQuery({ queryKey: ['opening', id], queryFn: () => getJobOpening(id) })
  const candidate = useQuery({ queryKey: ['candidate', id, candidateId], queryFn: () => getCandidate(id, candidateId) })
  const guide = useQuery({
    queryKey: key,
    // No guide yet is a normal state, not an error.
    queryFn: () => getInterviewGuide(id, candidateId).catch((e) => (e instanceof ApiError && e.status === 404 ? null : Promise.reject(e))),
  })

  const setGuide = (g: InterviewGuide) => {
    queryClient.setQueryData(key, g)
    queryClient.invalidateQueries({ queryKey: ['candidate', id, candidateId] })
  }
  // The first guide and a regeneration are different calls: generating again by accident would
  // throw away the Recruiter's edits and notes, so Hiring answers 409 to a second generate.
  const generate = useMutation({
    mutationFn: (emphasis: string) =>
      guide.data ? regenerateInterviewGuide(id, candidateId, emphasis) : generateInterviewGuide(id, candidateId, emphasis),
    onSuccess: setGuide,
    onError: (err) => notifications.show({ color: 'red', message: err.message }),
  })

  if (opening.isPending || candidate.isPending || guide.isPending) return <PageLoader />
  const failed = opening.error ?? candidate.error ?? guide.error
  if (failed) return <QueryError error={failed} />

  return (
    <Stack>
      <Button
        component={Link}
        to={`/job-openings/${id}?candidate=${candidateId}`}
        variant="subtle"
        size="xs"
        leftSection={<IconArrowLeft size={14} />}
        w="fit-content"
        px={4}
      >
        {opening.data!.title}
      </Button>
      <div>
        <Title order={2}>{t('guide.title')}</Title>
        <Text c="dimmed" size="sm">
          {t('guide.for', { resume: candidate.data!.file_name, opening: opening.data!.title })}
        </Text>
      </div>

      {guide.data ? (
        <GuideView guide={guide.data} opening={opening.data!} generating={generate.isPending} onRegenerate={(e) => generate.mutate(e)} onChange={setGuide} />
      ) : (
        <Paper withBorder p="xl">
          <Stack align="center">
            <Text c="dimmed">{t('guide.none')}</Text>
            <EmphasisForm loading={generate.isPending} label={t('guide.generate')} onSubmit={(e) => generate.mutate(e)} />
          </Stack>
        </Paper>
      )}
    </Stack>
  )
}

function EmphasisForm({ loading, label, onSubmit }: { loading: boolean; label: string; onSubmit: (emphasis: string) => void }) {
  const { t } = useI18n()
  const [emphasis, setEmphasis] = useState('')
  return (
    <Group align="flex-end">
      <TextInput
        label={t('guide.emphasis')}
        placeholder={t('guide.emphasisPlaceholder')}
        value={emphasis}
        onChange={(e) => setEmphasis(e.currentTarget.value)}
        w={300}
      />
      <Button leftSection={<IconSparkles size={16} />} loading={loading} onClick={() => onSubmit(emphasis)}>
        {label}
      </Button>
    </Group>
  )
}

function GuideView({
  guide,
  opening,
  generating,
  onRegenerate,
  onChange,
}: {
  guide: InterviewGuide
  opening: JobOpening
  generating: boolean
  onRegenerate: (emphasis: string) => void
  onChange: (guide: InterviewGuide) => void
}) {
  const { t, formatDate } = useI18n()
  const [regenerating, setRegenerating] = useState(false)
  const [editing, setEditing] = useState<InterviewQuestion | 'new' | null>(null)

  const revise = useMutation({
    mutationFn: (questions: QuestionDraft[]) => reviseGuideQuestions(guide.job_opening_id, guide.candidate_id, questions.map(toDraft)),
    onSuccess: (g) => {
      onChange(g)
      setEditing(null)
    },
    onError: (err) => notifications.show({ color: 'red', message: err.message }),
  })

  const saveQuestion = (q: QuestionDraft) =>
    revise.mutate(
      editing === 'new' ? [...guide.questions, q] : guide.questions.map((x) => (editing && x.id === editing.id ? q : x)),
    )

  return (
    <Stack>
      <Group justify="space-between">
        <Group gap="xs">
          <Text size="sm" c="dimmed">
            {t('guide.revision', { revision: guide.revision, date: formatDate(guide.updated_at) })}
          </Text>
          {guide.source === 'edited' && (
            <Badge variant="outline" color="gray" size="sm">
              {t('guide.edited')}
            </Badge>
          )}
          {guide.emphasis && (
            <Badge variant="light" size="sm">
              {t('guide.withEmphasis', { emphasis: guide.emphasis })}
            </Badge>
          )}
        </Group>
        <Group gap="xs">
          <Button variant="default" size="xs" leftSection={<IconPlus size={14} />} onClick={() => setEditing('new')}>
            {t('guide.addQuestion')}
          </Button>
          <Button variant="light" size="xs" leftSection={<IconRefresh size={14} />} onClick={() => setRegenerating(true)}>
            {t('guide.regenerate')}
          </Button>
        </Group>
      </Group>

      {generating && <Alert color="indigo">{t('guide.generating')}</Alert>}

      {guide.questions.map((q, i) => (
        <QuestionCard
          key={q.id}
          index={i}
          question={q}
          guide={guide}
          onEdit={() => setEditing(q)}
          onRemove={() => revise.mutate(guide.questions.filter((x) => x.id !== q.id))}
          onNoteSaved={onChange}
        />
      ))}

      <Modal opened={regenerating} onClose={() => setRegenerating(false)} title={t('guide.regenerate')} centered>
        <Stack>
          <Alert color="yellow">{t('guide.regenerateHint')}</Alert>
          <EmphasisForm
            loading={generating}
            label={t('guide.regenerate')}
            onSubmit={(e) => {
              onRegenerate(e)
              setRegenerating(false)
            }}
          />
        </Stack>
      </Modal>

      {editing && (
        <QuestionEditor
          question={editing === 'new' ? null : editing}
          opening={opening}
          saving={revise.isPending}
          onCancel={() => setEditing(null)}
          onSave={saveQuestion}
        />
      )}
    </Stack>
  )
}

function QuestionCard({
  index,
  question: q,
  guide,
  onEdit,
  onRemove,
  onNoteSaved,
}: {
  index: number
  question: InterviewQuestion
  guide: InterviewGuide
  onEdit: () => void
  onRemove: () => void
  onNoteSaved: (guide: InterviewGuide) => void
}) {
  const { t } = useI18n()
  const [note, setNote] = useState(q.note ?? '')
  const saveNote = useMutation({
    mutationFn: () => recordInterviewNote(guide.job_opening_id, guide.candidate_id, q.id, note),
    onSuccess: (g) => {
      onNoteSaved(g)
      notifications.show({ color: 'green', message: t('guide.noteSaved'), autoClose: 1500 })
    },
  })

  return (
    <Paper withBorder p="md" data-testid="question">
      <Group justify="space-between" align="flex-start" wrap="nowrap">
        <Stack gap={6} style={{ flex: 1 }}>
          <Group gap={6}>
            <Text c="dimmed" fw={600}>
              {index + 1}.
            </Text>
            <Badge color={KIND_COLOR[q.kind]} variant="light" size="sm">
              {t(`guide.kind.${q.kind}`)}
            </Badge>
            {q.criterion && (
              <Badge variant="outline" size="sm" color="gray">
                {t('guide.tests', { criterion: q.criterion })}
              </Badge>
            )}
          </Group>
          <Text fw={500}>{q.text}</Text>
          <Text size="xs" c="dimmed" fs="italic">
            {q.evidence ? t('guide.source', { evidence: q.evidence }) : t('guide.noEvidence')}
          </Text>
        </Stack>
        <Group gap={2} wrap="nowrap">
          <Tooltip label={t('guide.editQuestion')}>
            <ActionIcon variant="subtle" onClick={onEdit} aria-label={t('guide.editQuestion')}>
              <IconPencil size={16} />
            </ActionIcon>
          </Tooltip>
          <Tooltip label={t('guide.removeQuestion')}>
            <ActionIcon variant="subtle" color="red" onClick={onRemove} aria-label={t('guide.removeQuestion')}>
              <IconTrash size={16} />
            </ActionIcon>
          </Tooltip>
        </Group>
      </Group>
      <Textarea
        mt="sm"
        label={t('guide.note')}
        placeholder={t('guide.notePlaceholder')}
        autosize
        minRows={1}
        value={note}
        onChange={(e) => setNote(e.currentTarget.value)}
        onBlur={() => note !== (q.note ?? '') && saveNote.mutate()}
      />
    </Paper>
  )
}

function QuestionEditor({
  question,
  opening,
  saving,
  onCancel,
  onSave,
}: {
  question: InterviewQuestion | null
  opening: JobOpening
  saving: boolean
  onCancel: () => void
  onSave: (q: QuestionDraft) => void
}) {
  const { t } = useI18n()
  const [text, setText] = useState(question?.text ?? '')
  const [criterion, setCriterion] = useState<string | null>(question?.criterion || opening.criteria[0]?.name || null)
  const [kind, setKind] = useState<QuestionKind>(question?.kind ?? 'role-depth')

  return (
    <Modal opened onClose={onCancel} title={question ? t('guide.editQuestion') : t('guide.newQuestion')} centered size="lg">
      <Stack>
        <Textarea label={t('guide.questionText')} autosize minRows={3} value={text} onChange={(e) => setText(e.currentTarget.value)} data-autofocus />
        <Group grow>
          <Select label={t('criteria.name')} data={opening.criteria.map((c) => c.name)} value={criterion} onChange={setCriterion} />
          <Select
            label={t('criteria.classification')}
            data={KINDS.map((k) => ({ value: k, label: t(`guide.kind.${k}`) }))}
            value={kind}
            onChange={(v) => v && setKind(v as QuestionKind)}
          />
        </Group>
        {question?.note && text !== question.text && (
          <Alert color="yellow" py={6}>
            {t('guide.rewordDropsNote')}
          </Alert>
        )}
        <Group justify="flex-end">
          <Button variant="default" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
          <Button
            disabled={!text.trim()}
            loading={saving}
            onClick={() =>
              onSave({ text: text.trim(), criterion: criterion ?? '', kind, evidence: question?.evidence })
            }
          >
            {t('common.save')}
          </Button>
        </Group>
      </Stack>
    </Modal>
  )
}
