import { Button, Group, Paper, Table, Text, Title } from '@mantine/core'
import { IconPlus } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { listJobOpenings } from '../api/endpoints'
import { PageLoader, QueryError, StatusBadge } from '../components/common'
import { useI18n } from '../i18n/I18nProvider'

export function JobOpeningsPage() {
  const { t, formatDate } = useI18n()
  const navigate = useNavigate()
  const openings = useQuery({ queryKey: ['openings'], queryFn: listJobOpenings })

  return (
    <>
      <Group justify="space-between" mb="md">
        <Title order={2}>{t('openings.title')}</Title>
        <Button component={Link} to="/job-openings/new" leftSection={<IconPlus size={16} />}>
          {t('openings.new')}
        </Button>
      </Group>

      {openings.isPending ? (
        <PageLoader />
      ) : openings.isError ? (
        <QueryError error={openings.error} onRetry={() => openings.refetch()} />
      ) : openings.data.length === 0 ? (
        <Paper withBorder p="xl">
          <Text c="dimmed" ta="center">
            {t('openings.empty')}
          </Text>
        </Paper>
      ) : (
        <Paper withBorder>
          <Table highlightOnHover verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>{t('openings.col.title')}</Table.Th>
                <Table.Th>{t('openings.col.status')}</Table.Th>
                <Table.Th>{t('openings.col.criteria')}</Table.Th>
                <Table.Th visibleFrom="sm">{t('openings.col.timeToFill')}</Table.Th>
                <Table.Th visibleFrom="sm">{t('openings.col.opened')}</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {openings.data.map((o) => (
                <Table.Tr key={o.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/job-openings/${o.id}`)}>
                  <Table.Td>
                    <Text fw={600} component={Link} to={`/job-openings/${o.id}`} c="inherit" td="none">
                      {o.title}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <StatusBadge opening={o} />
                  </Table.Td>
                  <Table.Td>{o.criteria.length}</Table.Td>
                  <Table.Td visibleFrom="sm">{t('common.days', { count: o.expected_time_to_fill_days })}</Table.Td>
                  <Table.Td visibleFrom="sm">{formatDate(o.opened_at)}</Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Paper>
      )}
    </>
  )
}
