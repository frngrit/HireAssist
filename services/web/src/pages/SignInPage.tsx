import { Alert, Anchor, Button, Center, Code, Paper, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core'
import { IconAlertTriangle } from '@tabler/icons-react'
import { useState, type FormEvent } from 'react'
import { ApiError } from '../api/client'
import { changePassword } from '../api/endpoints'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthProvider'
import { useI18n } from '../i18n/I18nProvider'

const usingMocks = import.meta.env.VITE_USE_MOCKS !== 'false'

export function SignInPage() {
  const { member, signIn } = useAuth()
  const { t } = useI18n()
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as { from?: string } | null)?.from ?? '/job-openings'

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  // FR-0.5: a temporary password cannot open a session, so the member replaces it here first.
  const [mustChange, setMustChange] = useState(false)
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')

  if (member) return <Navigate to={from} replace />

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      if (mustChange) {
        if (newPassword !== confirmPassword) {
          setError(t('signIn.passwordsDiffer'))
          return
        }
        await changePassword(email, password, newPassword)
        await signIn(email, newPassword)
      } else {
        await signIn(email, password)
      }
      navigate(from, { replace: true })
    } catch (err) {
      if (!mustChange && err instanceof ApiError && err.status === 403) {
        setMustChange(true)
        return
      }
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  const fill = (demoEmail: string, demoPassword: string) => () => {
    setEmail(demoEmail)
    setPassword(demoPassword)
  }

  return (
    <Center mih="100vh" bg="var(--mantine-color-default-hover)" p="md">
      <Paper withBorder shadow="sm" p="xl" w="100%" maw={420}>
        <form onSubmit={submit}>
          <Stack>
            <div>
              <Title order={2} c="indigo">
                {t('app.name')}
              </Title>
              <Text c="dimmed" size="sm">
                {t('signIn.subtitle')}
              </Text>
            </div>
            <Title order={4}>{mustChange ? t('signIn.changeTitle') : t('signIn.title')}</Title>
            {mustChange && <Text size="sm">{t('signIn.changeHint')}</Text>}
            {error && (
              <Alert color="red" icon={<IconAlertTriangle size={16} />}>
                {error}
              </Alert>
            )}
            <TextInput
              label={t('signIn.email')}
              type="email"
              autoComplete="username"
              required
              readOnly={mustChange}
              value={email}
              onChange={(e) => setEmail(e.currentTarget.value)}
            />
            <PasswordInput
              label={mustChange ? t('signIn.temporaryPassword') : t('signIn.password')}
              autoComplete="current-password"
              required
              readOnly={mustChange}
              value={password}
              onChange={(e) => setPassword(e.currentTarget.value)}
            />
            {mustChange && (
              <>
                <PasswordInput
                  label={t('signIn.newPassword')}
                  description={t('signIn.newPasswordHint')}
                  autoComplete="new-password"
                  required
                  minLength={15}
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.currentTarget.value)}
                />
                <PasswordInput
                  label={t('signIn.confirmPassword')}
                  autoComplete="new-password"
                  required
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.currentTarget.value)}
                />
              </>
            )}
            <Button type="submit" loading={submitting} fullWidth>
              {mustChange ? t('signIn.changeSubmit') : t('signIn.submit')}
            </Button>
            {usingMocks && !mustChange && (
              <Text size="xs" c="dimmed">
                {t('signIn.demoAccounts')}{' '}
                <Anchor component="button" type="button" size="xs" onClick={fill('somchai@acme.example', 'somchai-recruiter-password')}>
                  <Code>somchai@acme.example</Code>
                </Anchor>{' '}
                ·{' '}
                <Anchor component="button" type="button" size="xs" onClick={fill('nok@acme.example', 'nok-admin-demo-password')}>
                  <Code>nok@acme.example</Code>
                </Anchor>
              </Text>
            )}
          </Stack>
        </form>
      </Paper>
    </Center>
  )
}
