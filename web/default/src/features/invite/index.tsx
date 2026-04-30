import { useCallback, useEffect, useState } from 'react'
import { Share2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getSelf, api } from '@/lib/api'
import dayjs from '@/lib/dayjs'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { SectionPageLayout } from '@/components/layout'
import { AffiliateRewardsCard } from '@/features/wallet/components/affiliate-rewards-card'
import { TransferDialog } from '@/features/wallet/components/dialogs/transfer-dialog'
import { useAffiliate } from '@/features/wallet/hooks'
import type { UserWalletData } from '@/features/wallet/types'

type InvitationRecord = {
  user_id: number
  username?: string
  display_name?: string
  total_contribution_rebate?: number | string
}

type InvitationRebate = {
  id: number
  rebate_amount?: number | string
  status?: string
  created_at?: number | string
  settled_at?: number | string
}

async function getInvitationRecords() {
  const res = await api.get('/api/user/aff/records', {
    params: { page_size: 100, _ts: Date.now() },
  })
  return (res.data?.data?.items || []) as InvitationRecord[]
}

async function getInvitationRebates() {
  const res = await api.get('/api/user/aff/rebates', {
    params: { page_size: 100, _ts: Date.now() },
  })
  return (res.data?.data?.items || []) as InvitationRebate[]
}

function formatMoney(value: number | string | undefined) {
  const amount = Number.parseFloat(String(value ?? 0))
  return `$${(Number.isFinite(amount) ? amount : 0).toFixed(2)}`
}

function formatTimestamp(value: number | string | undefined) {
  const timestamp = Number.parseInt(String(value ?? 0), 10)
  if (!Number.isFinite(timestamp) || timestamp <= 0) return '-'
  return dayjs(timestamp * 1000).format('YYYY-MM-DD HH:mm:ss')
}

export function Invite() {
  const { t } = useTranslation()
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [userLoading, setUserLoading] = useState(true)
  const [records, setRecords] = useState<InvitationRecord[]>([])
  const [rebates, setRebates] = useState<InvitationRebate[]>([])
  const [detailsLoading, setDetailsLoading] = useState(true)
  const [transferOpen, setTransferOpen] = useState(false)

  const {
    affiliateLink,
    loading: affiliateLoading,
    transferQuota,
    transferring,
  } = useAffiliate()

  const loadUser = useCallback(async () => {
    try {
      setUserLoading(true)
      const res = await getSelf()
      if (res.success && res.data) {
        setUser(res.data as UserWalletData)
      }
    } finally {
      setUserLoading(false)
    }
  }, [])

  const loadDetails = useCallback(async () => {
    try {
      setDetailsLoading(true)
      const [nextRecords, nextRebates] = await Promise.all([
        getInvitationRecords(),
        getInvitationRebates(),
      ])
      setRecords(nextRecords)
      setRebates(nextRebates)
    } finally {
      setDetailsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadUser()
    loadDetails()
  }, [loadUser, loadDetails])

  const handleTransfer = async (amount: number) => {
    const success = await transferQuota(amount)
    if (success) {
      await loadUser()
    }
    return success
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Referral Rewards')}</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t('Manage your referral link, rewards, and rebate records.')}
        </SectionPageLayout.Description>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-7xl flex-col gap-4'>
            <AffiliateRewardsCard
              user={user}
              affiliateLink={affiliateLink}
              onTransfer={() => setTransferOpen(true)}
              loading={affiliateLoading || userLoading}
            />

            <div className='grid gap-4 xl:grid-cols-2'>
              <Card>
                <CardHeader>
                  <CardTitle>{t('Referral Records')}</CardTitle>
                </CardHeader>
                <CardContent>
                  {detailsLoading ? (
                    <div className='text-muted-foreground py-10 text-center text-sm'>
                      {t('Loading...')}
                    </div>
                  ) : records.length > 0 ? (
                    <div className='divide-y rounded-lg border'>
                      {records.map((record) => (
                        <div
                          key={record.user_id}
                          className='grid grid-cols-[minmax(0,1fr)_auto] gap-3 p-3'
                        >
                          <div className='min-w-0'>
                            <div className='truncate text-sm font-medium'>
                              {record.display_name || record.username || '-'}
                            </div>
                            {record.display_name && record.username ? (
                              <div className='text-muted-foreground truncate text-xs'>
                                @{record.username}
                              </div>
                            ) : null}
                          </div>
                          <div className='text-sm font-semibold tabular-nums'>
                            {formatMoney(record.total_contribution_rebate)}
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <Empty>
                      <EmptyHeader>
                        <EmptyMedia variant='icon'>
                          <Share2 />
                        </EmptyMedia>
                        <EmptyTitle>{t('No referral records')}</EmptyTitle>
                        <EmptyDescription>
                          {t('Share your referral link to invite users.')}
                        </EmptyDescription>
                      </EmptyHeader>
                    </Empty>
                  )}
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>{t('Rebate Details')}</CardTitle>
                </CardHeader>
                <CardContent>
                  {detailsLoading ? (
                    <div className='text-muted-foreground py-10 text-center text-sm'>
                      {t('Loading...')}
                    </div>
                  ) : rebates.length > 0 ? (
                    <div className='divide-y rounded-lg border'>
                      {rebates.map((record) => (
                        <div key={record.id} className='grid gap-1 p-3'>
                          <div className='flex items-center justify-between gap-3'>
                            <span className='text-sm font-semibold tabular-nums'>
                              {formatMoney(record.rebate_amount)}
                            </span>
                            <span className='text-muted-foreground text-xs'>
                              {record.status || '-'}
                            </span>
                          </div>
                          <div className='text-muted-foreground text-xs'>
                            {t('Created')}: {formatTimestamp(record.created_at)}
                            {' · '}
                            {t('Settled')}: {formatTimestamp(record.settled_at)}
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <Empty>
                      <EmptyHeader>
                        <EmptyMedia variant='icon'>
                          <Share2 />
                        </EmptyMedia>
                        <EmptyTitle>{t('No rebate records')}</EmptyTitle>
                        <EmptyDescription>
                          {t(
                            'Rebate details will appear after referrals add funds.'
                          )}
                        </EmptyDescription>
                      </EmptyHeader>
                    </Empty>
                  )}
                </CardContent>
              </Card>
            </div>

            <Button
              type='button'
              variant='outline'
              className='w-fit'
              onClick={loadDetails}
              disabled={detailsLoading}
            >
              {t('Refresh')}
            </Button>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <TransferDialog
        open={transferOpen}
        onOpenChange={setTransferOpen}
        onConfirm={handleTransfer}
        availableQuota={user?.aff_quota ?? 0}
        transferring={transferring}
      />
    </>
  )
}
