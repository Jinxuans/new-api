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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import type { GrowthRewardItem } from '@/features/promotion/shared'
import { useStatus } from '@/hooks/use-status'

import { claimPromotionReward, promotionQueryKeys } from '../api'
import { createRewardPasswordSchema, type RewardPasswordForm } from '../schemas'

const JOIN_COMMUNITY_CODE = 'join_community'
const DAILY_CHECKIN_CODE = 'daily_checkin'

export function useTaskRewardClaim() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const queryClient = useQueryClient()
  const [passwordItem, setPasswordItem] = useState<GrowthRewardItem | null>(
    null
  )
  const [turnstileItem, setTurnstileItem] = useState<GrowthRewardItem | null>(
    null
  )
  const [turnstileKey, setTurnstileKey] = useState(0)
  const passwordForm = useForm<RewardPasswordForm>({
    resolver: zodResolver(createRewardPasswordSchema(t)),
    defaultValues: { password: '' },
  })
  const mutation = useMutation({
    mutationFn: (input: {
      code: string
      password?: string
      turnstileToken?: string
    }) =>
      claimPromotionReward(input.code, {
        password: input.password,
        turnstileToken: input.turnstileToken,
      }),
    onSuccess: async () => {
      toast.success(t('Reward claimed'))
      setPasswordItem(null)
      setTurnstileItem(null)
      passwordForm.reset()
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: promotionQueryKeys.rewardItems,
        }),
        queryClient.invalidateQueries({
          queryKey: promotionQueryKeys.overview,
        }),
        queryClient.invalidateQueries({
          queryKey: promotionQueryKeys.activityRoot,
        }),
      ])
    },
    onError: (error: Error) => {
      toast.error(error.message)
      setTurnstileKey((value) => value + 1)
    },
  })

  return {
    passwordItem,
    turnstileItem,
    turnstileKey,
    passwordForm,
    isPending: mutation.isPending,
    docsLink: String(status?.docs_link || ''),
    turnstileSiteKey: String(status?.turnstile_site_key || ''),
    claimItem(item: GrowthRewardItem) {
      if (item.code === JOIN_COMMUNITY_CODE) {
        passwordForm.reset()
        setPasswordItem(item)
        return
      }
      if (
        item.code === DAILY_CHECKIN_CODE &&
        status?.turnstile_check === true
      ) {
        if (!status.turnstile_site_key) {
          toast.error(t('Turnstile is enabled but site key is empty.'))
          return
        }
        setTurnstileItem(item)
        return
      }
      mutation.mutate({ code: item.code })
    },
    claimWithPassword(values: RewardPasswordForm) {
      if (passwordItem) {
        mutation.mutate({ code: passwordItem.code, password: values.password })
      }
    },
    claimAfterTurnstile(turnstileToken: string) {
      if (turnstileItem) {
        mutation.mutate({ code: turnstileItem.code, turnstileToken })
      }
    },
    closePassword() {
      setPasswordItem(null)
      passwordForm.reset()
    },
    closeTurnstile() {
      setTurnstileItem(null)
      setTurnstileKey((value) => value + 1)
    },
    resetTurnstile() {
      setTurnstileKey((value) => value + 1)
    },
  }
}

export type TaskRewardClaim = ReturnType<typeof useTaskRewardClaim>
