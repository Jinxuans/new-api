import { createFileRoute } from '@tanstack/react-router'
import { Invite } from '@/features/invite'

export const Route = createFileRoute('/_authenticated/invite/')({
  component: Invite,
})
