/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { formatQuota } from '@/lib/format'

import { promotionQueryKeys, transferAllReferralCredit } from '../api'

type ReferralTransferDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  quota: number
}

export function ReferralTransferDialog(props: ReferralTransferDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => transferAllReferralCredit(props.quota),
    onSuccess: async () => {
      props.onOpenChange(false)
      toast.success(t('Referral credit transferred to API balance'))
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: promotionQueryKeys.overview,
        }),
        queryClient.invalidateQueries({
          queryKey: promotionQueryKeys.activityRoot,
        }),
      ])
    },
    onError: (error: Error) => toast.error(error.message),
  })

  return (
    <AlertDialog open={props.open} onOpenChange={props.onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t('Transfer all referral credit?')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t(
              '{{quota}} will be added to your API balance. This action cannot be undone.',
              {
                quota: formatQuota(props.quota),
              }
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {t('Transfer all')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
