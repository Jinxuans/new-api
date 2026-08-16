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
import * as z from 'zod'

type Translate = (key: string) => string

export function createPromotionSubmissionSchema(t: Translate) {
  return z.object({
    itemCode: z.string().trim().min(1, t('Select a reward opportunity')),
    platform: z.string().trim().max(64, t('Platform is too long')),
    url: z
      .url(t('Enter a valid content URL'))
      .max(2_048, t('Content URL is too long')),
    remark: z.string().trim().max(500, t('Remark is too long')),
  })
}

export type PromotionSubmissionForm = z.infer<
  ReturnType<typeof createPromotionSubmissionSchema>
>

export function createRewardPasswordSchema(t: Translate) {
  return z.object({
    password: z.string().trim().min(1, t('Task password is required')),
  })
}

export type RewardPasswordForm = z.infer<
  ReturnType<typeof createRewardPasswordSchema>
>

export function createPromotionWithdrawalSchema(t: Translate) {
  return z.object({
    payoutMethod: z
      .string()
      .trim()
      .min(1, t('Payout method is required'))
      .max(32, t('Payout method is too long')),
    payoutAccount: z
      .string()
      .trim()
      .min(1, t('Payout account is required'))
      .max(200, t('Payout account is too long')),
    remark: z.string().trim().max(500, t('Remark is too long')),
  })
}

export type PromotionWithdrawalForm = z.infer<
  ReturnType<typeof createPromotionWithdrawalSchema>
>
