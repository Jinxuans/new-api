/*
Copyright (C) 2025 QuantumNous

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

import React, { useState } from 'react';
import {
  Avatar,
  Typography,
  Card,
  Button,
  Input,
  Badge,
  Space,
  Tabs,
  Empty,
} from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { Copy, Users, BarChart2, TrendingUp, Gift, Zap } from 'lucide-react';

const { Text } = Typography;

const TEXT = {
  inviteReward: '\u9080\u8bf7\u5956\u52b1',
  inviteExtraReward:
    '\u9080\u8bf7\u597d\u53cb\u83b7\u5f97\u989d\u5916\u5956\u52b1',
  statsTitle: '\u6536\u76ca\u7edf\u8ba1',
  transferToBalance: '\u5212\u8f6c\u5230\u4f59\u989d',
  pendingIncome: '\u5f85\u4f7f\u7528\u6536\u76ca',
  totalIncome: '\u603b\u6536\u76ca',
  inviteCount: '\u9080\u8bf7\u4eba\u6570',
  inviteLink: '\u9080\u8bf7\u94fe\u63a5',
  copy: '\u590d\u5236',
  inviteRecords: '\u9080\u8bf7\u8bb0\u5f55',
  rebateDetails: '\u8fd4\u4f63\u660e\u7ec6',
  user: '\u7528\u6237',
  totalContributionRebate: '\u7d2f\u8ba1\u8d21\u732e\u8fd4\u4f63',
  rebateAmount: '\u8fd4\u4f63\u91d1\u989d',
  status: '\u72b6\u6001',
  createdAt: '\u4ea7\u751f\u65f6\u95f4',
  settledAt: '\u5230\u8d26\u65f6\u95f4',
  loading: '\u52a0\u8f7d\u4e2d...',
  noData: '\u6682\u65e0\u6570\u636e',
  copyInviteLink:
    '\u590d\u5236\u9080\u8bf7\u94fe\u63a5\uff0c\u9080\u8bf7\u597d\u53cb',
  rewardDesc: '\u5956\u52b1\u8bf4\u660e',
  currentRebateRate: '\u5f53\u524d\u8fd4\u4f63\u6bd4\u4f8b:',
  currentInviteReward:
    '\u5f53\u524d\u6bcf\u9080\u8bf7 1 \u4eba\u5956\u52b1\u989d\u5ea6:',
  inviteRechargeReward:
    '\u9080\u8bf7\u597d\u53cb\u6ce8\u518c\uff0c\u597d\u53cb\u5145\u503c\u540e\u60a8\u53ef\u83b7\u5f97\u76f8\u5e94\u5956\u52b1',
  transferRewardToBalance:
    '\u901a\u8fc7\u5212\u8f6c\u529f\u80fd\u53ef\u5c06\u5956\u52b1\u989d\u5ea6\u8f6c\u5165\u5230\u8d26\u6237\u4f59\u989d\u4e2d',
  withdrawReward:
    '\u63d0\u73b0\u529f\u80fd\u53ef\u5c06\u5956\u52b1\u76f4\u63a5\u63d0\u53d6\u5230\u652f\u4ed8\u5b9d/\u5fae\u4fe1',
  moreInvitesMoreRewards:
    '\u9080\u8bf7\u7684\u597d\u53cb\u8d8a\u591a\uff0c\u83b7\u5f97\u7684\u5956\u52b1\u8d8a\u591a',
};

const InvitationCard = ({
  t,
  userState,
  renderQuota,
  setOpenTransfer,
  affLink,
  handleAffLinkClick,
  inviteRebatePercentageText,
  inviteRewardDisplayText,
  invitationRecords,
  invitationRecordsLoading,
  invitationRebates,
  invitationRebatesLoading,
  formatInviteRebateAmount,
  formatDateTime,
}) => {
  const [activeTab, setActiveTab] = useState('records');

  const inviteTabConfig = {
    records: {
      columns: [TEXT.user, TEXT.totalContributionRebate],
    },
    rebate: {
      columns: [TEXT.rebateAmount, TEXT.status, TEXT.createdAt, TEXT.settledAt],
    },
  };

  return (
    <Card className='!rounded-2xl shadow-sm border-0'>
      <div className='flex items-center mb-4'>
        <Avatar size='small' color='green' className='mr-3 shadow-md'>
          <Gift size={16} />
        </Avatar>
        <div>
          <Typography.Text className='text-lg font-medium'>
            {t(TEXT.inviteReward)}
          </Typography.Text>
          <div className='text-xs'>{t(TEXT.inviteExtraReward)}</div>
        </div>
      </div>

      <Space vertical style={{ width: '100%' }}>
        <Card
          className='!rounded-xl w-full'
          cover={
            <div
              className='relative h-30'
              style={{
                '--palette-primary-darkerChannel': '0 75 80',
                backgroundImage: `linear-gradient(0deg, rgba(var(--palette-primary-darkerChannel) / 80%), rgba(var(--palette-primary-darkerChannel) / 80%)), url('/cover-4.webp')`,
                backgroundSize: 'cover',
                backgroundPosition: 'center',
                backgroundRepeat: 'no-repeat',
              }}
            >
              <div className='relative z-10 h-full flex flex-col justify-between p-4'>
                <div className='flex justify-between items-center'>
                  <Text strong style={{ color: 'white', fontSize: '16px' }}>
                    {TEXT.statsTitle}
                  </Text>
                  <Button
                    type='primary'
                    theme='solid'
                    size='small'
                    disabled={
                      !userState?.user?.aff_quota ||
                      userState?.user?.aff_quota <= 0
                    }
                    onClick={() => setOpenTransfer(true)}
                    className='!rounded-lg'
                  >
                    <Zap size={12} className='mr-1' />
                    {TEXT.transferToBalance}
                  </Button>
                </div>

                <div className='grid grid-cols-3 gap-6 mt-4'>
                  <div className='text-center'>
                    <div
                      className='text-base sm:text-2xl font-bold mb-2'
                      style={{ color: 'white' }}
                    >
                      {renderQuota(userState?.user?.aff_quota || 0)}
                    </div>
                    <div className='flex items-center justify-center text-sm'>
                      <TrendingUp
                        size={14}
                        className='mr-1'
                        style={{ color: 'rgba(255,255,255,0.8)' }}
                      />
                      <Text
                        style={{
                          color: 'rgba(255,255,255,0.8)',
                          fontSize: '12px',
                        }}
                      >
                        {TEXT.pendingIncome}
                      </Text>
                    </div>
                  </div>

                  <div className='text-center'>
                    <div
                      className='text-base sm:text-2xl font-bold mb-2'
                      style={{ color: 'white' }}
                    >
                      {renderQuota(userState?.user?.aff_history_quota || 0)}
                    </div>
                    <div className='flex items-center justify-center text-sm'>
                      <BarChart2
                        size={14}
                        className='mr-1'
                        style={{ color: 'rgba(255,255,255,0.8)' }}
                      />
                      <Text
                        style={{
                          color: 'rgba(255,255,255,0.8)',
                          fontSize: '12px',
                        }}
                      >
                        {TEXT.totalIncome}
                      </Text>
                    </div>
                  </div>

                  <div className='text-center'>
                    <div
                      className='text-base sm:text-2xl font-bold mb-2'
                      style={{ color: 'white' }}
                    >
                      {userState?.user?.aff_count || 0}
                    </div>
                    <div className='flex items-center justify-center text-sm'>
                      <Users
                        size={14}
                        className='mr-1'
                        style={{ color: 'rgba(255,255,255,0.8)' }}
                      />
                      <Text
                        style={{
                          color: 'rgba(255,255,255,0.8)',
                          fontSize: '12px',
                        }}
                      >
                        {t(TEXT.inviteCount)}
                      </Text>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          }
        >
          <Input
            value={affLink}
            readOnly
            className='!rounded-lg'
            prefix={t(TEXT.inviteLink)}
            suffix={
              <Button
                type='primary'
                theme='solid'
                onClick={handleAffLinkClick}
                icon={<Copy size={14} />}
                className='!rounded-lg'
              >
                {t(TEXT.copy)}
              </Button>
            }
          />
        </Card>

        <div className='grid grid-cols-1 min-[1000px]:grid-cols-[minmax(0,1.75fr)_minmax(280px,0.65fr)] gap-4 w-full items-start'>
          <Card className='!rounded-xl w-full overflow-hidden'>
            <Tabs
              type='line'
              activeKey={activeTab}
              onChange={setActiveTab}
              tabBarStyle={{ marginBottom: 0 }}
            >
              <Tabs.TabPane tab={TEXT.inviteRecords} itemKey='records'>
                <div>
                  <div className='grid grid-cols-2 gap-4 px-1 py-4 border-b border-[var(--semi-color-border)]'>
                    {inviteTabConfig.records.columns.map((column) => (
                      <Text
                        key={column}
                        strong
                        type='tertiary'
                        className='text-sm'
                      >
                        {column}
                      </Text>
                    ))}
                  </div>

                  {invitationRecordsLoading ? (
                    <div className='flex items-center justify-center px-4 py-10 sm:py-12 text-center min-h-[320px]'>
                      <Text type='tertiary'>{t(TEXT.loading)}</Text>
                    </div>
                  ) : invitationRecords?.length > 0 ? (
                    <div className='min-h-[320px]'>
                      {invitationRecords.map((record) => {
                        const displayName =
                          record?.display_name || record?.username || '-';
                        const showUsername =
                          record?.display_name &&
                          record?.username &&
                          record.display_name !== record.username;

                        return (
                          <div
                            key={record.user_id}
                            className='grid grid-cols-2 gap-4 px-1 py-4 border-b last:border-b-0 border-[var(--semi-color-border)] items-center'
                          >
                            <div className='min-w-0'>
                              <Text strong className='block truncate'>
                                {displayName}
                              </Text>
                              {showUsername ? (
                                <Text
                                  type='tertiary'
                                  className='block text-xs truncate mt-1'
                                >
                                  @{record.username}
                                </Text>
                              ) : null}
                            </div>
                            <Text strong>
                              {formatInviteRebateAmount(
                                record?.total_contribution_rebate,
                              )}
                            </Text>
                          </div>
                        );
                      })}
                    </div>
                  ) : (
                    <div className='flex flex-col items-center justify-center px-4 py-10 sm:py-12 text-center min-h-[320px]'>
                      <Empty
                        image={
                          <IllustrationNoResult
                            style={{ width: 220, height: 220 }}
                          />
                        }
                        darkModeImage={
                          <IllustrationNoResultDark
                            style={{ width: 220, height: 220 }}
                          />
                        }
                        description={t(TEXT.noData)}
                      />

                      <Button
                        type='primary'
                        theme='solid'
                        icon={<Copy size={14} />}
                        onClick={handleAffLinkClick}
                        className='!rounded-full mt-2'
                      >
                        {TEXT.copyInviteLink}
                      </Button>
                    </div>
                  )}
                </div>
              </Tabs.TabPane>

              <Tabs.TabPane tab={TEXT.rebateDetails} itemKey='rebate'>
                <div>
                  <div className='grid grid-cols-4 gap-4 px-1 py-4 border-b border-[var(--semi-color-border)]'>
                    {inviteTabConfig.rebate.columns.map((column) => (
                      <Text
                        key={column}
                        strong
                        type='tertiary'
                        className='text-sm'
                      >
                        {column}
                      </Text>
                    ))}
                  </div>

                  {invitationRebatesLoading ? (
                    <div className='flex items-center justify-center px-4 py-10 sm:py-12 text-center min-h-[320px]'>
                      <Text type='tertiary'>{t(TEXT.loading)}</Text>
                    </div>
                  ) : invitationRebates?.length > 0 ? (
                    <div className='min-h-[320px]'>
                      {invitationRebates.map((record) => (
                        <div
                          key={record.id}
                          className='grid grid-cols-4 gap-4 px-1 py-4 border-b last:border-b-0 border-[var(--semi-color-border)] items-center'
                        >
                          <Text strong>
                            {formatInviteRebateAmount(record?.rebate_amount)}
                          </Text>
                          <Text>{record?.status || '-'}</Text>
                          <Text>{formatDateTime(record?.created_at)}</Text>
                          <Text>{formatDateTime(record?.settled_at)}</Text>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <div className='flex flex-col items-center justify-center px-4 py-10 sm:py-12 text-center min-h-[320px]'>
                      <Empty
                        image={
                          <IllustrationNoResult
                            style={{ width: 220, height: 220 }}
                          />
                        }
                        darkModeImage={
                          <IllustrationNoResultDark
                            style={{ width: 220, height: 220 }}
                          />
                        }
                        description={t(TEXT.noData)}
                      />

                      <Button
                        type='primary'
                        theme='solid'
                        icon={<Copy size={14} />}
                        onClick={handleAffLinkClick}
                        className='!rounded-full mt-2'
                      >
                        {TEXT.copyInviteLink}
                      </Button>
                    </div>
                  )}
                </div>
              </Tabs.TabPane>
            </Tabs>
          </Card>

          <Card
            className='!rounded-xl w-full self-start'
            title={<Text type='tertiary'>{t(TEXT.rewardDesc)}</Text>}
          >
            <div className='space-y-3'>
              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {TEXT.currentRebateRate}
                  <Text strong className='text-sm ml-1'>
                    {inviteRebatePercentageText}%
                  </Text>
                </Text>
              </div>

              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {TEXT.currentInviteReward}
                  <Text strong className='text-sm ml-1'>
                    {inviteRewardDisplayText}
                  </Text>
                </Text>
              </div>

              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {t(TEXT.inviteRechargeReward)}
                </Text>
              </div>

              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {TEXT.transferRewardToBalance}
                </Text>
              </div>

              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {TEXT.withdrawReward}
                </Text>
              </div>

              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {t(TEXT.moreInvitesMoreRewards)}
                </Text>
              </div>
            </div>
          </Card>
        </div>
      </Space>
    </Card>
  );
};

export default InvitationCard;
