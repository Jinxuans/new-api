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
import { ExternalLink, Pencil, RefreshCw, Save } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { rewardItemCopy } from '@/features/promotion/shared'
import { api } from '@/lib/api'
import { formatQuota } from '@/lib/format'

import { SettingsSection } from '../components/settings-section'

const MONTHLY_SPEND_CODE = 'monthly_spend_target'

type AdminGrowthRewardItem = {
  id: number
  code: string
  title: string
  description: string
  introduction?: string
  reward_quota: number
  effective_reward_quota_min: number
  effective_reward_quota_max: number
  reward_source: 'reward_program' | 'item_override'
  item_type: string
  action_url?: string
  enabled: boolean
  effective_enabled: boolean
  once_per_user: boolean
  daily_limit: number
  built_in: boolean
  claim_password_configured: boolean
  capabilities: {
    reward_override: boolean
    action_url: boolean
    introduction: boolean
    claim_password: boolean
    daily_limit: boolean
  }
}

type RewardItemDraft = {
  custom_reward: boolean
  reward_quota: string
  introduction: string
  action_url: string
  claim_password: string
  enabled: boolean
  daily_limit: string
}

function toDraft(item: AdminGrowthRewardItem): RewardItemDraft {
  return {
    custom_reward: item.reward_source === 'item_override',
    reward_quota: String(
      item.reward_quota || item.effective_reward_quota_min || 0
    ),
    introduction: item.introduction || '',
    action_url: item.action_url || '',
    claim_password: '',
    enabled: item.enabled !== false,
    daily_limit: String(item.daily_limit || 0),
  }
}

export function GrowthRewardItemsSection() {
  const { t } = useTranslation()
  const [items, setItems] = useState<AdminGrowthRewardItem[]>([])
  const [selectedItem, setSelectedItem] =
    useState<AdminGrowthRewardItem | null>(null)
  const [draft, setDraft] = useState<RewardItemDraft | null>(null)
  const [loading, setLoading] = useState(true)
  const [savingId, setSavingId] = useState<number | null>(null)

  const loadItems = useCallback(async () => {
    try {
      setLoading(true)
      const res = await api.get('/api/growth/admin/items')
      setItems((res.data?.data || []) as AdminGrowthRewardItem[])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadItems()
  }, [loadItems])

  const counts = useMemo(() => {
    const enabled = items.filter((item) => item.effective_enabled).length
    const custom = items.filter(
      (item) =>
        item.capabilities.reward_override &&
        item.reward_source === 'item_override'
    ).length
    return { enabled, custom }
  }, [items])

  const openEditor = (item: AdminGrowthRewardItem) => {
    setSelectedItem(item)
    setDraft(toDraft(item))
  }

  const closeEditor = () => {
    if (savingId !== null) return
    setSelectedItem(null)
    setDraft(null)
  }

  const updateDraft = (patch: Partial<RewardItemDraft>) => {
    setDraft((current) => (current ? { ...current, ...patch } : current))
  }

  const saveSelectedItem = async () => {
    if (!selectedItem || !draft) return

    const customRewardQuota = Math.max(0, Number(draft.reward_quota || 0))
    if (
      selectedItem.capabilities.reward_override &&
      draft.custom_reward &&
      customRewardQuota <= 0
    ) {
      toast.error(t('Custom reward quota is required'))
      return
    }

    let rewardQuota = 0
    if (selectedItem.capabilities.reward_override && draft.custom_reward) {
      rewardQuota = customRewardQuota
    }

    const payload: Record<string, unknown> = {
      title: selectedItem.title,
      description: selectedItem.description,
      introduction: selectedItem.capabilities.introduction
        ? draft.introduction.trim()
        : '',
      reward_quota: rewardQuota,
      action_url: selectedItem.capabilities.action_url
        ? draft.action_url.trim()
        : '',
      enabled: draft.enabled,
      daily_limit: selectedItem.capabilities.daily_limit
        ? Math.max(0, Number(draft.daily_limit || 0))
        : 0,
    }

    if (
      selectedItem.capabilities.claim_password &&
      draft.claim_password.trim()
    ) {
      payload.claim_password = draft.claim_password.trim()
    }

    try {
      setSavingId(selectedItem.id)
      const res = await api.put(
        `/api/growth/admin/items/${selectedItem.id}`,
        payload
      )
      if (res.data?.success) {
        toast.success(t('Reward item updated'))
        setSelectedItem(null)
        setDraft(null)
        await loadItems()
      }
    } finally {
      setSavingId(null)
    }
  }

  const titleFor = (item: AdminGrowthRewardItem) =>
    t(rewardItemCopy[item.code]?.title || item.title || item.code)

  const rewardSourceLabel = (item: AdminGrowthRewardItem) => {
    return item.reward_source === 'item_override'
      ? t('Task override')
      : t('Reward program')
  }

  const rewardValueLabel = (item: AdminGrowthRewardItem) => {
    if (item.effective_reward_quota_min !== item.effective_reward_quota_max) {
      return t('{{min}} to {{max}}', {
        min: formatQuota(item.effective_reward_quota_min),
        max: formatQuota(item.effective_reward_quota_max),
      })
    }
    return formatQuota(item.effective_reward_quota_min)
  }

  const configLabels = (item: AdminGrowthRewardItem) => {
    const labels: string[] = []
    if (item.capabilities.action_url) {
      labels.push(t('Community link'))
    }
    if (item.capabilities.introduction) labels.push(t('Task introduction'))
    if (item.capabilities.claim_password) {
      labels.push(
        item.claim_password_configured
          ? t('Claim password set')
          : t('Claim password not set')
      )
    }
    if (item.capabilities.daily_limit) {
      labels.push(
        item.daily_limit && item.daily_limit > 0
          ? t('Daily limit {{count}}', { count: item.daily_limit })
          : t('Optional daily limit')
      )
    }
    if (item.code === MONTHLY_SPEND_CODE) labels.push(t('Monthly target'))
    return labels
  }

  return (
    <SettingsSection
      title={t('Reward Task Items')}
      description={t(
        'Control task visibility and task-specific settings. Global reward rules stay in Promotion & Rewards.'
      )}
    >
      <div className='rounded-lg border p-4'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div className='flex flex-wrap items-center gap-2'>
            <Badge variant='secondary'>
              {t('Enabled')}: {counts.enabled}
            </Badge>
            <Badge variant='outline'>
              {t('Custom rewards')}: {counts.custom}
            </Badge>
          </div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={loadItems}
            disabled={loading}
          >
            <RefreshCw className='size-4' />
            {t('Refresh')}
          </Button>
        </div>

        <Separator className='my-4' />

        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Reward item')}</TableHead>
                <TableHead>{t('Reward source')}</TableHead>
                <TableHead>{t('Task-specific settings')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.length > 0 ? (
                items.map((item) => {
                  const labels = configLabels(item)
                  let statusLabel = t('Disabled')
                  if (item.effective_enabled) {
                    statusLabel = t('Enabled')
                  } else if (item.enabled) {
                    statusLabel = t('Disabled by program')
                  }
                  return (
                    <TableRow key={item.id}>
                      <TableCell className='min-w-64 whitespace-normal'>
                        <div className='flex flex-col gap-1'>
                          <div className='flex flex-wrap items-center gap-2'>
                            <span className='font-medium'>
                              {titleFor(item)}
                            </span>
                            <Badge variant='outline'>{item.item_type}</Badge>
                          </div>
                          <div className='text-muted-foreground font-mono text-xs'>
                            {item.code}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className='min-w-48 whitespace-normal'>
                        <div className='flex flex-col gap-1'>
                          <Badge variant='outline' className='w-fit'>
                            {rewardSourceLabel(item)}
                          </Badge>
                          <span className='text-muted-foreground text-xs'>
                            {rewardValueLabel(item)}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className='min-w-64 whitespace-normal'>
                        {labels.length > 0 ? (
                          <div className='flex flex-wrap gap-1.5'>
                            {labels.map((label) => (
                              <Badge key={label} variant='secondary'>
                                {label}
                              </Badge>
                            ))}
                          </div>
                        ) : (
                          <span className='text-muted-foreground text-xs'>
                            {t('No task-specific fields')}
                          </span>
                        )}
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            item.effective_enabled ? 'default' : 'outline'
                          }
                        >
                          {statusLabel}
                        </Badge>
                      </TableCell>
                      <TableCell className='text-right'>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          onClick={() => openEditor(item)}
                        >
                          <Pencil className='size-4' />
                          {t('Edit')}
                        </Button>
                      </TableCell>
                    </TableRow>
                  )
                })
              ) : (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className='text-muted-foreground py-10 text-center'
                  >
                    {loading ? t('Loading...') : t('No reward items')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      <Dialog
        open={Boolean(selectedItem && draft)}
        onOpenChange={(open) => {
          if (!open) closeEditor()
        }}
      >
        {selectedItem && draft ? (
          <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-2xl'>
            <DialogHeader>
              <DialogTitle>{t('Edit reward item')}</DialogTitle>
              <DialogDescription>{titleFor(selectedItem)}</DialogDescription>
            </DialogHeader>

            <div className='flex flex-col gap-5'>
              <div className='flex items-center justify-between gap-4 rounded-lg border p-3'>
                <div className='flex min-w-0 flex-col gap-1'>
                  <div className='text-sm font-medium'>{t('Task enabled')}</div>
                  <div className='text-muted-foreground text-xs'>
                    {t('Disabled tasks are hidden from user reward pages.')}
                  </div>
                </div>
                <Switch
                  checked={draft.enabled}
                  aria-label={t('Task enabled')}
                  onCheckedChange={(checked) =>
                    updateDraft({ enabled: checked })
                  }
                />
              </div>

              {selectedItem.capabilities.reward_override ? (
                <div className='rounded-lg border p-3'>
                  <div className='flex items-start justify-between gap-4'>
                    <div className='flex min-w-0 flex-col gap-1'>
                      <div className='text-sm font-medium'>
                        {t('Reward source')}
                      </div>
                      <div className='text-muted-foreground text-xs'>
                        {t(
                          'Use the global rule by default, or override only this task.'
                        )}
                      </div>
                    </div>
                    <label className='flex shrink-0 items-center gap-2 text-xs'>
                      <Switch
                        checked={draft.custom_reward}
                        aria-label={t('Use a task-specific reward')}
                        onCheckedChange={(checked) =>
                          updateDraft({ custom_reward: checked })
                        }
                      />
                      {t('Custom')}
                    </label>
                  </div>
                  {draft.custom_reward ? (
                    <div className='mt-3 grid gap-1.5'>
                      <label className='text-xs font-medium'>
                        {t('Custom reward quota')}
                      </label>
                      <Input
                        aria-label={t('Custom reward quota')}
                        type='number'
                        min={1}
                        value={draft.reward_quota}
                        onChange={(event) =>
                          updateDraft({ reward_quota: event.target.value })
                        }
                      />
                    </div>
                  ) : (
                    <div className='text-muted-foreground mt-3 text-xs'>
                      {t('Current reward from the reward program: {{amount}}', {
                        amount: rewardValueLabel(selectedItem),
                      })}
                    </div>
                  )}
                </div>
              ) : (
                <div className='rounded-lg border p-3'>
                  <div className='text-sm font-medium'>
                    {t('Reward source')}
                  </div>
                  <div className='text-muted-foreground mt-1 text-xs'>
                    {t('Current reward from the reward program: {{amount}}', {
                      amount: rewardValueLabel(selectedItem),
                    })}
                  </div>
                </div>
              )}

              <TaskSpecificFields
                item={selectedItem}
                draft={draft}
                updateDraft={updateDraft}
              />
            </div>

            <DialogFooter>
              <Button type='button' variant='outline' onClick={closeEditor}>
                {t('Cancel')}
              </Button>
              <Button
                type='button'
                onClick={saveSelectedItem}
                disabled={savingId === selectedItem.id}
              >
                <Save className='size-4' />
                {savingId === selectedItem.id ? t('Saving...') : t('Save')}
              </Button>
            </DialogFooter>
          </DialogContent>
        ) : null}
      </Dialog>
    </SettingsSection>
  )
}

function TaskSpecificFields({
  item,
  draft,
  updateDraft,
}: {
  item: AdminGrowthRewardItem
  draft: RewardItemDraft
  updateDraft: (patch: Partial<RewardItemDraft>) => void
}) {
  const { t } = useTranslation()
  const hasActionUrl = item.capabilities.action_url
  const hasIntroduction = item.capabilities.introduction
  const hasPassword = item.capabilities.claim_password
  const hasDailyLimit = item.capabilities.daily_limit

  if (!hasActionUrl && !hasIntroduction && !hasPassword && !hasDailyLimit) {
    return (
      <div className='rounded-lg border p-3'>
        <div className='text-sm font-medium'>{t('Task-specific settings')}</div>
        <div className='text-muted-foreground mt-1 text-xs'>
          {item.code === MONTHLY_SPEND_CODE
            ? t('Monthly spend target is configured in Promotion & Rewards.')
            : t(
                'This task only needs availability and reward source settings.'
              )}
        </div>
      </div>
    )
  }

  return (
    <div className='rounded-lg border p-3'>
      <div className='flex flex-col gap-1'>
        <div className='text-sm font-medium'>{t('Task-specific settings')}</div>
        <div className='text-muted-foreground text-xs'>
          {t('Only settings used by this task type are shown.')}
        </div>
      </div>
      <Separator className='my-3' />
      <div className='grid gap-4'>
        {hasIntroduction ? (
          <div className='grid gap-1.5'>
            <label className='text-xs font-medium'>
              {t('Task introduction')}
            </label>
            <Textarea
              aria-label={t('Task introduction')}
              value={draft.introduction}
              onChange={(event) =>
                updateDraft({ introduction: event.target.value })
              }
              placeholder={t(
                'Shown on the user reward page. Use one paragraph per line.'
              )}
              className='min-h-28'
            />
            <p className='text-muted-foreground text-xs'>
              {t('Shown on the user reward page for this task.')}
            </p>
          </div>
        ) : null}

        {hasActionUrl ? (
          <div className='grid gap-1.5'>
            <label className='text-xs font-medium'>{t('Community link')}</label>
            <div className='flex gap-2'>
              <Input
                aria-label={t('Community link')}
                value={draft.action_url}
                onChange={(event) =>
                  updateDraft({ action_url: event.target.value })
                }
                placeholder='https://'
              />
              {/^(https?):\/\//i.test(draft.action_url) ? (
                <Button
                  type='button'
                  variant='outline'
                  size='icon'
                  aria-label={t('Open community link')}
                  render={
                    <a
                      href={draft.action_url}
                      target='_blank'
                      rel='noreferrer'
                    />
                  }
                >
                  <ExternalLink className='size-4' />
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}

        {hasPassword ? (
          <div className='grid gap-1.5'>
            <label className='text-xs font-medium'>
              {t('New claim password')}
            </label>
            <Input
              aria-label={t('New claim password')}
              type='password'
              value={draft.claim_password}
              onChange={(event) =>
                updateDraft({ claim_password: event.target.value })
              }
              placeholder={t('Leave blank to keep the current password')}
            />
          </div>
        ) : null}

        {hasDailyLimit ? (
          <div className='grid gap-1.5'>
            <label className='text-xs font-medium'>{t('Daily limit')}</label>
            <Input
              aria-label={t('Daily limit')}
              type='number'
              min={0}
              max={1000}
              value={draft.daily_limit}
              onChange={(event) =>
                updateDraft({ daily_limit: event.target.value })
              }
            />
            <p className='text-muted-foreground text-xs'>
              {t('Zero means unlimited submissions per user per day.')}
            </p>
          </div>
        ) : null}
      </div>
    </div>
  )
}
