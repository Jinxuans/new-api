import { createFileRoute } from '@tanstack/react-router'
import { AppHeader, Main } from '@/components/layout'
import { CreationLauncher } from '@/features/creation'

export const Route = createFileRoute('/_authenticated/creation/')({
  component: CreationPage,
})

function CreationPage() {
  return (
    <>
      <AppHeader />
      <Main className='p-0'>
        <CreationLauncher />
      </Main>
    </>
  )
}
