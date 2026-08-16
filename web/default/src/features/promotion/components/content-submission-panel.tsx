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
import { File02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { useStatus } from '@/hooks/use-status'

import {
  createPromotionSubmission,
  getPromotionRewardItems,
  promotionQueryKeys,
} from '../api'
import { getRewardAmount, getRewardTitle } from '../reward-display'
import {
  createPromotionSubmissionSchema,
  type PromotionSubmissionForm,
} from '../schemas'
import {
  PromotionSectionError,
  PromotionSectionSkeleton,
} from './section-state'

const JOIN_COMMUNITY_CODE = 'join_community'

export function ContentSubmissionPanel() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const queryClient = useQueryClient()
  const itemsQuery = useQuery({
    queryKey: promotionQueryKeys.rewardItems,
    queryFn: getPromotionRewardItems,
  })
  const items = useMemo(
    () =>
      (itemsQuery.data || []).filter(
        (item) => item.item_type !== 'auto' && item.code !== JOIN_COMMUNITY_CODE
      ),
    [itemsQuery.data]
  )
  const form = useForm<PromotionSubmissionForm>({
    resolver: zodResolver(createPromotionSubmissionSchema(t)),
    defaultValues: { itemCode: '', platform: '', url: '', remark: '' },
  })
  const submissionMutation = useMutation({
    mutationFn: (values: PromotionSubmissionForm) =>
      createPromotionSubmission({
        item_code: values.itemCode,
        platform: values.platform,
        url: values.url,
        remark: values.remark,
      }),
    onSuccess: async () => {
      toast.success(t('Submission created'))
      form.reset()
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: promotionQueryKeys.rewardItems,
        }),
        queryClient.invalidateQueries({
          queryKey: promotionQueryKeys.activityRoot,
        }),
      ])
    },
    onError: (error: Error) => toast.error(error.message),
  })

  let content
  if (itemsQuery.isLoading) {
    content = <PromotionSectionSkeleton />
  } else if (itemsQuery.isError) {
    content = <PromotionSectionError onRetry={() => itemsQuery.refetch()} />
  } else if (items.length === 0) {
    content = (
      <p className='text-muted-foreground text-sm'>
        {t('No content reward opportunities are available right now.')}
      </p>
    )
  } else {
    content = (
      <form
        onSubmit={form.handleSubmit((values) =>
          submissionMutation.mutate(values)
        )}
      >
        <FieldGroup>
          <Controller
            control={form.control}
            name='itemCode'
            render={({ field, fieldState }) => (
              <Field data-invalid={fieldState.invalid}>
                <FieldLabel htmlFor='promotion-reward-item'>
                  {t('Reward opportunity')}
                </FieldLabel>
                <Select
                  items={items.map((item) => ({
                    value: item.code,
                    label: getRewardTitle(item, t),
                  }))}
                  value={field.value || null}
                  onValueChange={(value) => field.onChange(value || '')}
                >
                  <SelectTrigger
                    id='promotion-reward-item'
                    className='w-full'
                    aria-invalid={fieldState.invalid}
                  >
                    <SelectValue
                      placeholder={t('Select a reward opportunity')}
                    />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {items.map((item) => (
                        <SelectItem key={item.code} value={item.code}>
                          {getRewardTitle(item, t)} · {getRewardAmount(item)}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldError errors={[fieldState.error]} />
              </Field>
            )}
          />
          <Field data-invalid={Boolean(form.formState.errors.platform)}>
            <FieldLabel htmlFor='promotion-platform'>
              {t('Platform')}
            </FieldLabel>
            <Input
              id='promotion-platform'
              placeholder={t('For example: YouTube, blog, or community')}
              aria-invalid={Boolean(form.formState.errors.platform)}
              {...form.register('platform')}
            />
            <FieldError errors={[form.formState.errors.platform]} />
          </Field>
          <Field data-invalid={Boolean(form.formState.errors.url)}>
            <FieldLabel htmlFor='promotion-content-url'>
              {t('Content URL')}
            </FieldLabel>
            <Input
              id='promotion-content-url'
              type='url'
              placeholder='https://'
              aria-invalid={Boolean(form.formState.errors.url)}
              {...form.register('url')}
            />
            <FieldError errors={[form.formState.errors.url]} />
          </Field>
          <Field data-invalid={Boolean(form.formState.errors.remark)}>
            <FieldLabel htmlFor='promotion-submission-remark'>
              {t('Remark')}
            </FieldLabel>
            <Textarea
              id='promotion-submission-remark'
              rows={3}
              placeholder={t(
                'Add context that helps the reviewer verify your work'
              )}
              aria-invalid={Boolean(form.formState.errors.remark)}
              {...form.register('remark')}
            />
            <FieldError errors={[form.formState.errors.remark]} />
          </Field>
          <Button
            type='submit'
            disabled={
              status?.growth_submission_enabled !== true ||
              submissionMutation.isPending
            }
          >
            {t('Submit for review')}
          </Button>
          {status?.growth_submission_enabled !== true ? (
            <FieldDescription>
              {t('Content submissions are currently paused.')}
            </FieldDescription>
          ) : null}
        </FieldGroup>
      </form>
    )
  }

  return (
    <Card data-card-hover='false'>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          <HugeiconsIcon icon={File02Icon} strokeWidth={2} />
          {t('Content rewards')}
        </CardTitle>
        <CardDescription>
          {t('Choose one opportunity and submit a public link for review.')}
        </CardDescription>
      </CardHeader>
      <CardContent>{content}</CardContent>
    </Card>
  )
}
