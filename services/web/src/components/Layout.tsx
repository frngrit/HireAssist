import { Alert, AppShell, Badge, Button, Container, Group, SegmentedControl, Text, Title, UnstyledButton } from '@mantine/core'
import { IconFlask, IconLogout } from '@tabler/icons-react'
import { Link, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthProvider'
import { useI18n, type Language } from '../i18n/I18nProvider'

const usingMocks = import.meta.env.VITE_USE_MOCKS !== 'false'

export function Layout() {
  const { member, signOut } = useAuth()
  const { t, language, setLanguage } = useI18n()
  const navigate = useNavigate()

  return (
    <AppShell header={{ height: 60 }} padding="md">
      <AppShell.Header>
        <Container size="xl" h="100%">
          <Group h="100%" justify="space-between" wrap="nowrap">
            <Group gap="lg" wrap="nowrap">
              <UnstyledButton component={Link} to="/job-openings">
                <Title order={3} c="indigo">
                  {t('app.name')}
                </Title>
              </UnstyledButton>
              <Button component={Link} to="/job-openings" variant="subtle" visibleFrom="sm">
                {t('nav.jobOpenings')}
              </Button>
            </Group>
            <Group gap="sm" wrap="nowrap">
              <SegmentedControl
                size="xs"
                value={language}
                onChange={(value) => setLanguage(value as Language)}
                data={[
                  { label: 'EN', value: 'en' },
                  { label: 'ไทย', value: 'th' },
                ]}
                aria-label="Language"
              />
              {member && (
                <Group gap={6} visibleFrom="sm" wrap="nowrap">
                  <Text size="sm" c="dimmed">
                    {member.email}
                  </Text>
                  <Badge variant="light" size="sm">
                    {member.role}
                  </Badge>
                </Group>
              )}
              <Button
                variant="default"
                size="xs"
                leftSection={<IconLogout size={14} />}
                onClick={() => {
                  signOut()
                  navigate('/sign-in')
                }}
              >
                {t('nav.signOut')}
              </Button>
            </Group>
          </Group>
        </Container>
      </AppShell.Header>
      <AppShell.Main>
        <Container size="xl">
          {usingMocks && (
            <Alert variant="light" color="yellow" icon={<IconFlask size={16} />} mb="md" py={6}>
              {t('app.mockBanner')}
            </Alert>
          )}
          <Outlet />
        </Container>
      </AppShell.Main>
    </AppShell>
  )
}
