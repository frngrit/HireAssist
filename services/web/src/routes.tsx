import { Center, Loader } from '@mantine/core'
import type { ReactNode } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from './auth/AuthProvider'
import { Layout } from './components/Layout'
import { InterviewGuidePage } from './pages/InterviewGuidePage'
import { JobOpeningPage } from './pages/JobOpeningPage'
import { JobOpeningsPage } from './pages/JobOpeningsPage'
import { NewJobOpeningPage } from './pages/NewJobOpeningPage'
import { SignInPage } from './pages/SignInPage'

/** Every page but sign-in needs a session (FR-0.2); without one, go to sign-in and come back. */
function RequireSession({ children }: { children: ReactNode }) {
  const { member } = useAuth()
  const location = useLocation()
  if (member === undefined)
    return (
      <Center h="100vh">
        <Loader />
      </Center>
    )
  if (!member) return <Navigate to="/sign-in" replace state={{ from: location.pathname }} />
  return <>{children}</>
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/sign-in" element={<SignInPage />} />
      <Route
        element={
          <RequireSession>
            <Layout />
          </RequireSession>
        }
      >
        <Route path="/" element={<Navigate to="/job-openings" replace />} />
        <Route path="/job-openings" element={<JobOpeningsPage />} />
        <Route path="/job-openings/new" element={<NewJobOpeningPage />} />
        <Route path="/job-openings/:id" element={<JobOpeningPage />} />
        <Route
          path="/job-openings/:id/candidates/:candidateId/interview-guide"
          element={<InterviewGuidePage />}
        />
        <Route path="*" element={<Navigate to="/job-openings" replace />} />
      </Route>
    </Routes>
  )
}
