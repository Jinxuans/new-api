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
  Pagination,
  Skeleton,
} from '@douyinfe/semi-ui';
import {
  IllustrationFailure,
  IllustrationFailureDark,
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import {
  Copy,
  Users,
  BarChart2,
  TrendingUp,
  Gift,
  RefreshCw,
  Zap,
} from 'lucide-react';

const { Text } = Typography;

const REBATE_STATUS_CONFIG = {
  pending: { type: 'warning', key: '待结算' },
  settled: { type: 'success', key: '已结算' },
  frozen: { type: 'warning', key: '已冻结' },
  reversed: { type: 'danger', key: '已撤销' },
};

const InvitationCard = ({
  t,
  userState,
  renderQuota,
  setOpenTransfer,
  affLink,
  handleAffLinkClick,
  complianceConfirmed = false,
  complianceMessage,
  inviteRebatePercentageText,
  inviteRewardDisplayText,
  invitationRecords = [],
  invitationRecordsLoading = false,
  invitationRecordsError = '',
  invitationRecordsPage = 1,
  invitationRecordsPageSize = 10,
  invitationRecordsTotal = 0,
  onInvitationRecordsPageChange,
  onRetryInvitationRecords,
  invitationRebates = [],
  invitationRebatesLoading = false,
  invitationRebatesError = '',
  invitationRebatesPage = 1,
  invitationRebatesPageSize = 10,
  invitationRebatesTotal = 0,
  onInvitationRebatesPageChange,
  onRetryInvitationRebates,
  formatInviteRebateAmount,
  formatDateTime,
}) => {
  const [activeTab, setActiveTab] = useState('records');

  return (
    <Card className='!rounded-2xl shadow-sm border-0'>
      <div className='flex items-center mb-4'>
        <Avatar size='small' color='green' className='mr-3 shadow-md'>
          <Gift size={16} />
        </Avatar>
        <div>
          <Typography.Text className='text-lg font-medium'>
            {t('邀请奖励')}
          </Typography.Text>
          <div className='text-xs'>{t('邀请好友获得额外奖励')}</div>
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
                <div className='flex justify-between items-center gap-3'>
                  <Text strong style={{ color: 'white', fontSize: '16px' }}>
                    {t('收益统计')}
                  </Text>
                  <Button
                    type='primary'
                    theme='solid'
                    size='small'
                    disabled={
                      !complianceConfirmed ||
                      !userState?.user?.aff_quota ||
                      userState.user.aff_quota <= 0
                    }
                    onClick={() => setOpenTransfer(true)}
                    className='!rounded-lg'
                  >
                    <Zap size={12} className='mr-1' />
                    {t('划转到余额')}
                  </Button>
                </div>
                {!complianceConfirmed ? (
                  <Text
                    style={{
                      color: 'rgba(255,255,255,0.8)',
                      fontSize: 12,
                    }}
                  >
                    {complianceMessage || t('邀请奖励划转暂不可用。')}
                  </Text>
                ) : null}

                <div className='grid grid-cols-3 gap-3 sm:gap-6 mt-4'>
                  <div className='text-center min-w-0'>
                    <div
                      className='text-base sm:text-2xl font-bold mb-2 truncate'
                      style={{ color: 'white' }}
                    >
                      {renderQuota(userState?.user?.aff_quota || 0)}
                    </div>
                    <div className='flex items-center justify-center text-sm'>
                      <TrendingUp
                        size={14}
                        className='mr-1 shrink-0'
                        style={{ color: 'rgba(255,255,255,0.8)' }}
                      />
                      <Text
                        style={{
                          color: 'rgba(255,255,255,0.8)',
                          fontSize: '12px',
                        }}
                      >
                        {t('待使用收益')}
                      </Text>
                    </div>
                  </div>

                  <div className='text-center min-w-0'>
                    <div
                      className='text-base sm:text-2xl font-bold mb-2 truncate'
                      style={{ color: 'white' }}
                    >
                      {renderQuota(userState?.user?.aff_history_quota || 0)}
                    </div>
                    <div className='flex items-center justify-center text-sm'>
                      <BarChart2
                        size={14}
                        className='mr-1 shrink-0'
                        style={{ color: 'rgba(255,255,255,0.8)' }}
                      />
                      <Text
                        style={{
                          color: 'rgba(255,255,255,0.8)',
                          fontSize: '12px',
                        }}
                      >
                        {t('总收益')}
                      </Text>
                    </div>
                  </div>

                  <div className='text-center min-w-0'>
                    <div
                      className='text-base sm:text-2xl font-bold mb-2 truncate'
                      style={{ color: 'white' }}
                    >
                      {userState?.user?.aff_count || 0}
                    </div>
                    <div className='flex items-center justify-center text-sm'>
                      <Users
                        size={14}
                        className='mr-1 shrink-0'
                        style={{ color: 'rgba(255,255,255,0.8)' }}
                      />
                      <Text
                        style={{
                          color: 'rgba(255,255,255,0.8)',
                          fontSize: '12px',
                        }}
                      >
                        {t('邀请人数')}
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
            prefix={t('邀请链接')}
            placeholder={t('正在获取邀请链接...')}
            suffix={
              <Button
                type='primary'
                theme='solid'
                disabled={!affLink}
                onClick={handleAffLinkClick}
                icon={<Copy size={14} />}
                className='!rounded-lg'
              >
                {t('复制')}
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
              <Tabs.TabPane tab={t('邀请记录')} itemKey='records'>
                <div className='overflow-x-auto'>
                  <table className='w-full min-w-[560px] text-left'>
                    <thead>
                      <tr className='border-b border-[var(--semi-color-border)]'>
                        <th className='px-1 py-4 font-normal'>
                          <Text strong type='tertiary' className='text-sm'>
                            {t('用户')}
                          </Text>
                        </th>
                        <th className='px-1 py-4 font-normal'>
                          <Text strong type='tertiary' className='text-sm'>
                            {t('注册时间')}
                          </Text>
                        </th>
                        <th className='px-1 py-4 font-normal'>
                          <Text strong type='tertiary' className='text-sm'>
                            {t('累计返佣额度')}
                          </Text>
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {invitationRecordsLoading ? (
                        <tr>
                          <td colSpan={3} className='px-4 py-10 min-h-[320px]'>
                            <Skeleton.Paragraph active rows={5} />
                          </td>
                        </tr>
                      ) : null}
                      {!invitationRecordsLoading && invitationRecordsError ? (
                        <tr>
                          <td colSpan={3}>
                            <div className='flex min-h-[320px] flex-col items-center justify-center px-4 py-10 text-center'>
                              <Empty
                                image={
                                  <IllustrationFailure
                                    style={{ width: 180, height: 180 }}
                                  />
                                }
                                darkModeImage={
                                  <IllustrationFailureDark
                                    style={{ width: 180, height: 180 }}
                                  />
                                }
                                description={invitationRecordsError}
                              />
                              <Button
                                type='primary'
                                theme='outline'
                                icon={<RefreshCw size={14} />}
                                onClick={onRetryInvitationRecords}
                                className='!rounded-lg mt-4'
                              >
                                {t('重新加载')}
                              </Button>
                            </div>
                          </td>
                        </tr>
                      ) : null}
                      {!invitationRecordsLoading &&
                      !invitationRecordsError &&
                      invitationRecords.length > 0
                        ? invitationRecords.map((record) => {
                            const displayName =
                              record?.display_name || record?.username || '-';
                            const showUsername =
                              record?.display_name &&
                              record?.username &&
                              record.display_name !== record.username;

                            return (
                              <tr
                                key={`${record.username}:${record.created_at}`}
                                className='border-b last:border-b-0 border-[var(--semi-color-border)]'
                              >
                                <td className='px-1 py-4 min-w-0'>
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
                                </td>
                                <td className='px-1 py-4'>
                                  <Text>
                                    {formatDateTime(record?.created_at)}
                                  </Text>
                                </td>
                                <td className='px-1 py-4'>
                                  <Text strong>
                                    {renderQuota(
                                      record?.total_rebate_quota || 0,
                                    )}
                                  </Text>
                                </td>
                              </tr>
                            );
                          })
                        : null}
                      {!invitationRecordsLoading &&
                      !invitationRecordsError &&
                      invitationRecords.length === 0 ? (
                        <tr>
                          <td colSpan={3}>
                            <div className='flex min-h-[320px] flex-col items-center justify-center px-4 py-10 text-center'>
                              <Empty
                                image={
                                  <IllustrationNoResult
                                    style={{ width: 180, height: 180 }}
                                  />
                                }
                                darkModeImage={
                                  <IllustrationNoResultDark
                                    style={{ width: 180, height: 180 }}
                                  />
                                }
                                description={t('暂无数据')}
                              />
                              <Button
                                type='primary'
                                theme='solid'
                                icon={<Copy size={14} />}
                                disabled={!affLink}
                                onClick={handleAffLinkClick}
                                className='!rounded-full mt-2'
                              >
                                {t('复制邀请链接，邀请好友')}
                              </Button>
                            </div>
                          </td>
                        </tr>
                      ) : null}
                    </tbody>
                  </table>
                </div>
                {!invitationRecordsLoading &&
                !invitationRecordsError &&
                invitationRecordsTotal > 0 ? (
                  <div className='flex flex-col gap-3 border-t border-[var(--semi-color-border)] px-1 pt-4 sm:flex-row sm:items-center sm:justify-between'>
                    <Text type='tertiary' className='text-sm'>
                      {t('共 {{total}} 条', { total: invitationRecordsTotal })}
                    </Text>
                    {invitationRecordsTotal > invitationRecordsPageSize ? (
                      <Pagination
                        currentPage={invitationRecordsPage}
                        pageSize={invitationRecordsPageSize}
                        total={invitationRecordsTotal}
                        size='small'
                        onPageChange={onInvitationRecordsPageChange}
                      />
                    ) : null}
                  </div>
                ) : null}
              </Tabs.TabPane>

              <Tabs.TabPane tab={t('返佣明细')} itemKey='rebate'>
                <div className='overflow-x-auto'>
                  <table className='w-full min-w-[760px] text-left'>
                    <thead>
                      <tr className='border-b border-[var(--semi-color-border)]'>
                        {[
                          t('来源好友'),
                          t('奖励'),
                          t('状态'),
                          t('产生时间'),
                          t('到账时间'),
                        ].map((column) => (
                          <th key={column} className='px-1 py-4 font-normal'>
                            <Text strong type='tertiary' className='text-sm'>
                              {column}
                            </Text>
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {invitationRebatesLoading ? (
                        <tr>
                          <td colSpan={5} className='px-4 py-10 min-h-[320px]'>
                            <Skeleton.Paragraph active rows={5} />
                          </td>
                        </tr>
                      ) : null}
                      {!invitationRebatesLoading && invitationRebatesError ? (
                        <tr>
                          <td colSpan={5}>
                            <div className='flex min-h-[320px] flex-col items-center justify-center px-4 py-10 text-center'>
                              <Empty
                                image={
                                  <IllustrationFailure
                                    style={{ width: 180, height: 180 }}
                                  />
                                }
                                darkModeImage={
                                  <IllustrationFailureDark
                                    style={{ width: 180, height: 180 }}
                                  />
                                }
                                description={invitationRebatesError}
                              />
                              <Button
                                type='primary'
                                theme='outline'
                                icon={<RefreshCw size={14} />}
                                onClick={onRetryInvitationRebates}
                                className='!rounded-lg mt-4'
                              >
                                {t('重新加载')}
                              </Button>
                            </div>
                          </td>
                        </tr>
                      ) : null}
                      {!invitationRebatesLoading &&
                      !invitationRebatesError &&
                      invitationRebates.length > 0
                        ? invitationRebates.map((record) => {
                            const statusConfig = REBATE_STATUS_CONFIG[
                              record?.status
                            ] || {
                              type: 'primary',
                              key: record?.status || '-',
                            };

                            return (
                              <tr
                                key={`${record.invitee_name}:${record.created_at}:${record.rebate_amount_minor}:${record.rebate_currency}`}
                                className='border-b last:border-b-0 border-[var(--semi-color-border)]'
                              >
                                <td className='px-1 py-4'>
                                  <Text strong>
                                    {record?.invitee_name || '-'}
                                  </Text>
                                </td>
                                <td className='px-1 py-4'>
                                  <Text strong className='block'>
                                    {formatInviteRebateAmount(record)}
                                  </Text>
                                  <Text type='tertiary' className='text-xs'>
                                    {record?.cashable === false
                                      ? t('邀请额度')
                                      : t('现金返佣')}
                                  </Text>
                                </td>
                                <td className='px-1 py-4'>
                                  <span className='flex items-center gap-2'>
                                    <Badge dot type={statusConfig.type} />
                                    <Text>{t(statusConfig.key)}</Text>
                                  </span>
                                </td>
                                <td className='px-1 py-4'>
                                  <Text>
                                    {formatDateTime(record?.created_at)}
                                  </Text>
                                </td>
                                <td className='px-1 py-4'>
                                  <Text>
                                    {formatDateTime(record?.settled_at)}
                                  </Text>
                                </td>
                              </tr>
                            );
                          })
                        : null}
                      {!invitationRebatesLoading &&
                      !invitationRebatesError &&
                      invitationRebates.length === 0 ? (
                        <tr>
                          <td colSpan={5}>
                            <div className='flex min-h-[320px] flex-col items-center justify-center px-4 py-10 text-center'>
                              <Empty
                                image={
                                  <IllustrationNoResult
                                    style={{ width: 180, height: 180 }}
                                  />
                                }
                                darkModeImage={
                                  <IllustrationNoResultDark
                                    style={{ width: 180, height: 180 }}
                                  />
                                }
                                description={t('暂无数据')}
                              />
                            </div>
                          </td>
                        </tr>
                      ) : null}
                    </tbody>
                  </table>
                </div>
                {!invitationRebatesLoading &&
                !invitationRebatesError &&
                invitationRebatesTotal > 0 ? (
                  <div className='flex flex-col gap-3 border-t border-[var(--semi-color-border)] px-1 pt-4 sm:flex-row sm:items-center sm:justify-between'>
                    <Text type='tertiary' className='text-sm'>
                      {t('共 {{total}} 条', { total: invitationRebatesTotal })}
                    </Text>
                    {invitationRebatesTotal > invitationRebatesPageSize ? (
                      <Pagination
                        currentPage={invitationRebatesPage}
                        pageSize={invitationRebatesPageSize}
                        total={invitationRebatesTotal}
                        size='small'
                        onPageChange={onInvitationRebatesPageChange}
                      />
                    ) : null}
                  </div>
                ) : null}
              </Tabs.TabPane>
            </Tabs>
          </Card>

          <Card
            className='!rounded-xl w-full self-start'
            title={<Text type='tertiary'>{t('奖励说明')}</Text>}
          >
            <div className='space-y-3'>
              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {t('当前返佣比例:')}
                  <Text strong className='text-sm ml-1'>
                    {inviteRebatePercentageText}
                    {inviteRebatePercentageText === '—' ? '' : '%'}
                  </Text>
                </Text>
              </div>

              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {t('当前每邀请 1 人奖励额度:')}
                  <Text strong className='text-sm ml-1'>
                    {inviteRewardDisplayText}
                  </Text>
                </Text>
              </div>

              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {t('邀请好友注册，好友充值后您可获得相应奖励')}
                </Text>
              </div>

              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {t('通过划转功能可将奖励额度转入到账户余额中')}
                </Text>
              </div>

              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {t('邀请额度和现金返佣分开记录，具体可用操作以页面显示为准')}
                </Text>
              </div>

              <div className='flex items-start gap-2'>
                <Badge dot type='success' />
                <Text type='tertiary' className='text-sm'>
                  {t('邀请的好友越多，获得的奖励越多')}
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
