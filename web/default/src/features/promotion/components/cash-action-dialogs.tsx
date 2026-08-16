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
import { useForm } from 'react-hook-form'
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
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { formatCashCents } from '@/features/promotion/shared'
import { formatQuota } from '@/lib/format'

import {
  convertAllCashToBalance,
  promotionQueryKeys,
  withdrawAllCash,
  type PromotionEarnings,
} from '../api'
import {
  createPromotionWithdrawalSchema,
  type PromotionWithdrawalForm,
} from '../schemas'

type CashActionDialogsProps = {
  earnings: PromotionEarnings
  conversionOpen: boolean
  onConversionOpenChange: (open: boolean) => void
  withdrawalOpen: boolean
  onWithdrawalOpenChange: (open: boolean) => void
}

export function CashActionDialogs(props: CashActionDialogsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<PromotionWithdrawalForm>({
    resolver: zodResolver(createPromotionWithdrawalSchema(t)),
    defaultValues: { payoutMethod: '', payoutAccount: '', remark: '' },
  })
  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: promotionQueryKeys.overview }),
      queryClient.invalidateQueries({
        queryKey: promotionQueryKeys.activityRoot,
      }),
    ])
  }
  const conversionMutation = useMutation({
    mutationFn: convertAllCashToBalance,
    onSuccess: async () => {
      props.onConversionOpenChange(false)
      toast.success(t('Cash commission converted to API balance'))
      await refresh()
    },
    onError: (error: Error) => toast.error(error.message),
  })
  const withdrawalMutation = useMutation({
    mutationFn: (values: PromotionWithdrawalForm) =>
      withdrawAllCash({
        payout_method: values.payoutMethod,
        payout_account: values.payoutAccount,
        remark: values.remark,
      }),
    onSuccess: async () => {
      props.onWithdrawalOpenChange(false)
      form.reset()
      toast.success(t('Withdrawal request submitted'))
      await refresh()
    },
    onError: (error: Error) => toast.error(error.message),
  })
  const cash = formatCashCents(
    props.earnings.withdrawableCashCents,
    props.earnings.cashCurrency
  )
  const quota = formatQuota(props.earnings.cashQuotaEquivalent)

  return (
    <>
      <AlertDialog
        open={props.conversionOpen}
        onOpenChange={props.onConversionOpenChange}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Convert all cash commission to API balance?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                '{{cash}} will be converted into {{quota}} of API balance. This action cannot be undone.',
                {
                  cash,
                  quota,
                }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={conversionMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={conversionMutation.isPending}
              onClick={() => conversionMutation.mutate()}
            >
              {t('Convert all cash')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog
        open={props.withdrawalOpen}
        onOpenChange={(open) => {
          if (!open) form.reset()
          props.onWithdrawalOpenChange(open)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Withdraw all cash commission?')}</DialogTitle>
            <DialogDescription>
              {t(
                '{{cash}} (equivalent to {{quota}} of API balance) will be locked while the withdrawal is reviewed.',
                {
                  cash,
                  quota,
                }
              )}
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={form.handleSubmit((values) =>
              withdrawalMutation.mutate(values)
            )}
          >
            <FieldGroup>
              <Field data-invalid={Boolean(form.formState.errors.payoutMethod)}>
                <FieldLabel htmlFor='promotion-payout-method'>
                  {t('Payout method')}
                </FieldLabel>
                <Input
                  id='promotion-payout-method'
                  placeholder={t(
                    'For example: Alipay, WeChat, or bank transfer'
                  )}
                  aria-invalid={Boolean(form.formState.errors.payoutMethod)}
                  {...form.register('payoutMethod')}
                />
                <FieldError errors={[form.formState.errors.payoutMethod]} />
              </Field>
              <Field
                data-invalid={Boolean(form.formState.errors.payoutAccount)}
              >
                <FieldLabel htmlFor='promotion-payout-account'>
                  {t('Payout account')}
                </FieldLabel>
                <Input
                  id='promotion-payout-account'
                  autoComplete='off'
                  aria-invalid={Boolean(form.formState.errors.payoutAccount)}
                  {...form.register('payoutAccount')}
                />
                <FieldError errors={[form.formState.errors.payoutAccount]} />
              </Field>
              <Field data-invalid={Boolean(form.formState.errors.remark)}>
                <FieldLabel htmlFor='promotion-withdrawal-remark'>
                  {t('Remark')}
                </FieldLabel>
                <Textarea
                  id='promotion-withdrawal-remark'
                  rows={3}
                  aria-invalid={Boolean(form.formState.errors.remark)}
                  {...form.register('remark')}
                />
                <FieldError errors={[form.formState.errors.remark]} />
              </Field>
              <DialogFooter>
                <Button
                  type='button'
                  variant='outline'
                  disabled={withdrawalMutation.isPending}
                  onClick={() => props.onWithdrawalOpenChange(false)}
                >
                  {t('Cancel')}
                </Button>
                <Button type='submit' disabled={withdrawalMutation.isPending}>
                  {t('Confirm withdrawal of all cash')}
                </Button>
              </DialogFooter>
            </FieldGroup>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}
