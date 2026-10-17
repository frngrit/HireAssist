import { ActionIcon, Button, Group, NumberInput, SegmentedControl, Stack, Table, Text, TextInput } from '@mantine/core'
import { IconPlus, IconTrash } from '@tabler/icons-react'
import type { Classification, Criterion } from '../api/types'
import { useI18n } from '../i18n/I18nProvider'

/** Edits a list of criteria in place: add, edit, remove, reclassify, reweight (FR-1.4). */
export function CriteriaEditor({ value, onChange }: { value: Criterion[]; onChange: (next: Criterion[]) => void }) {
  const { t } = useI18n()
  const update = (index: number, patch: Partial<Criterion>) =>
    onChange(value.map((c, i) => (i === index ? { ...c, ...patch } : c)))

  return (
    <Stack gap="xs">
      <Table verticalSpacing="xs" withRowBorders={false}>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>{t('criteria.name')}</Table.Th>
            <Table.Th w={230}>{t('criteria.classification')}</Table.Th>
            <Table.Th w={100}>{t('criteria.weight')}</Table.Th>
            <Table.Th w={40} />
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {value.map((c, i) => (
            <Table.Tr key={i}>
              <Table.Td>
                <TextInput
                  value={c.name}
                  onChange={(e) => update(i, { name: e.currentTarget.value })}
                  aria-label={`${t('criteria.name')} ${i + 1}`}
                  error={!c.name.trim()}
                />
              </Table.Td>
              <Table.Td>
                <SegmentedControl
                  fullWidth
                  size="xs"
                  value={c.classification}
                  onChange={(v) => update(i, { classification: v as Classification })}
                  data={[
                    { value: 'must-have', label: t('classification.must-have') },
                    { value: 'nice-to-have', label: t('classification.nice-to-have') },
                  ]}
                />
              </Table.Td>
              <Table.Td>
                <NumberInput
                  min={1}
                  max={10}
                  value={c.weight}
                  onChange={(v) => update(i, { weight: typeof v === 'number' ? v : 1 })}
                  aria-label={`${t('criteria.weight')} ${i + 1}`}
                  allowDecimal={false}
                />
              </Table.Td>
              <Table.Td>
                <ActionIcon
                  variant="subtle"
                  color="red"
                  onClick={() => onChange(value.filter((_, j) => j !== i))}
                  aria-label={t('criteria.remove')}
                >
                  <IconTrash size={16} />
                </ActionIcon>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      <Group justify="space-between">
        <Button
          variant="light"
          size="xs"
          leftSection={<IconPlus size={14} />}
          onClick={() => onChange([...value, { name: '', classification: 'nice-to-have', weight: 3 }])}
        >
          {t('criteria.add')}
        </Button>
        <Text size="xs" c="dimmed">
          {t('criteria.mustHaveHint')}
        </Text>
      </Group>
    </Stack>
  )
}

/** A read-only view of criteria. */
export function CriteriaList({ criteria }: { criteria: Criterion[] }) {
  const { t } = useI18n()
  return (
    <Table>
      <Table.Thead>
        <Table.Tr>
          <Table.Th>{t('criteria.name')}</Table.Th>
          <Table.Th>{t('criteria.classification')}</Table.Th>
          <Table.Th>{t('criteria.weight')}</Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {criteria.map((c, i) => (
          <Table.Tr key={i}>
            <Table.Td>{c.name}</Table.Td>
            <Table.Td>
              <Text size="sm" c={c.classification === 'must-have' ? 'red.7' : 'dimmed'} fw={c.classification === 'must-have' ? 600 : 400}>
                {t(`classification.${c.classification}`)}
              </Text>
            </Table.Td>
            <Table.Td>{c.weight}</Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  )
}
