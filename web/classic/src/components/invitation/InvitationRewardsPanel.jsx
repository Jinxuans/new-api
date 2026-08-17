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

import React, {
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { useTranslation } from 'react-i18next';
import {
  API,
  copy,
  formatDateTimeString,
  formatMinorAmount,
  getCurrencyConfig,
  getQuotaPerUnit,
  renderQuota,
  setUserData,
  showError,
  showSuccess,
} from '../../helpers';
import {
  displayAmountToQuota,
  quotaToDisplayAmount,
} from '../../helpers/quota';
import { UserContext } from '../../context/User';
import { StatusContext } from '../../context/Status';
import InvitationCard from '../topup/InvitationCard';
import TransferModal from '../topup/modals/TransferModal';

const PAGE_SIZE = 10;

const InvitationRewardsPanel = ({ className = '' }) => {
  const { t } = useTranslation();
  const [userState, userDispatch] = useContext(UserContext);
  const [statusState] = useContext(StatusContext);
  const [affLink, setAffLink] = useState('');
  const [openTransfer, setOpenTransfer] = useState(false);
  const [transferAmount, setTransferAmount] = useState(0);
  const [complianceState, setComplianceState] = useState('loading');

  const [invitationRecords, setInvitationRecords] = useState([]);
  const [invitationRecordsLoading, setInvitationRecordsLoading] =
    useState(false);
  const [invitationRecordsError, setInvitationRecordsError] = useState('');
  const [invitationRecordsPage, setInvitationRecordsPage] = useState(1);
  const [invitationRecordsTotal, setInvitationRecordsTotal] = useState(0);

  const [invitationRebates, setInvitationRebates] = useState([]);
  const [invitationRebatesLoading, setInvitationRebatesLoading] =
    useState(false);
  const [invitationRebatesError, setInvitationRebatesError] = useState('');
  const [invitationRebatesPage, setInvitationRebatesPage] = useState(1);
  const [invitationRebatesTotal, setInvitationRebatesTotal] = useState(0);

  const affFetchedRef = useRef(false);
  const invitationRecordsRequestRef = useRef(0);
  const invitationRebatesRequestRef = useRef(0);
  const minTransferAmount = useMemo(
    () => quotaToDisplayAmount(getQuotaPerUnit()),
    [],
  );

  const inviteRebatePercentageText = useMemo(() => {
    const rawValue = statusState?.status?.invite_rebate_percentage;
    const parsedValue = Number.parseFloat(rawValue);
    if (!Number.isFinite(parsedValue)) {
      return '—';
    }
    return Number.isInteger(parsedValue)
      ? String(parsedValue)
      : parsedValue.toFixed(1).replace(/\.0$/, '');
  }, [statusState?.status?.invite_rebate_percentage]);

  const inviteRewardDisplayText = useMemo(() => {
    const quota = Number.parseFloat(statusState?.status?.quota_for_inviter);
    return Number.isFinite(quota) ? renderQuota(quota) : '—';
  }, [statusState?.status?.quota_for_inviter]);

  const getUserQuota = useCallback(async () => {
    try {
      const res = await API.get('/api/user/self');
      const { success, message, data } = res.data;
      if (success) {
        userDispatch({ type: 'login', payload: data });
        setUserData(data);
      } else {
        showError(message || t('加载账户余额失败'));
      }
    } catch (error) {
      showError(
        error?.response?.data?.message ||
          error?.message ||
          t('加载账户余额失败'),
      );
    }
  }, [t, userDispatch]);

  const getPaymentComplianceStatus = useCallback(async () => {
    setComplianceState('loading');
    try {
      const res = await API.get('/api/user/topup/info');
      const { success, data } = res.data;
      if (!success || !data) {
        setComplianceState('error');
        return;
      }
      setComplianceState(
        data.payment_compliance_confirmed === true ? 'confirmed' : 'blocked',
      );
    } catch {
      setComplianceState('error');
    }
  }, []);

  const getAffLink = useCallback(async () => {
    try {
      const res = await API.get('/api/user/aff', {
        params: { _ts: Date.now() },
      });
      const { success, message, data } = res.data;
      if (success) {
        setAffLink(`${window.location.origin}/register?aff=${data}`);
      } else {
        showError(message || t('加载邀请链接失败'));
      }
    } catch (error) {
      showError(
        error?.response?.data?.message ||
          error?.message ||
          t('加载邀请链接失败'),
      );
    }
  }, [t]);

  const getInvitationRecords = useCallback(
    async (requestedPage) => {
      const requestId = ++invitationRecordsRequestRef.current;
      setInvitationRecordsLoading(true);
      setInvitationRecordsError('');
      try {
        const res = await API.get('/api/user/aff/records', {
          params: {
            p: requestedPage,
            page_size: PAGE_SIZE,
            _ts: Date.now(),
          },
        });
        if (requestId !== invitationRecordsRequestRef.current) {
          return;
        }
        const { success, message, data } = res.data;
        if (!success) {
          setInvitationRecords([]);
          setInvitationRecordsTotal(0);
          setInvitationRecordsError(message || t('加载邀请记录失败'));
          return;
        }
        setInvitationRecords(data?.items || []);
        setInvitationRecordsTotal(Number(data?.total) || 0);
      } catch (error) {
        if (requestId !== invitationRecordsRequestRef.current) {
          return;
        }
        setInvitationRecords([]);
        setInvitationRecordsTotal(0);
        setInvitationRecordsError(
          error?.response?.data?.message ||
            error?.message ||
            t('加载邀请记录失败'),
        );
      } finally {
        if (requestId === invitationRecordsRequestRef.current) {
          setInvitationRecordsLoading(false);
        }
      }
    },
    [t],
  );

  const getInvitationRebates = useCallback(
    async (requestedPage) => {
      const requestId = ++invitationRebatesRequestRef.current;
      setInvitationRebatesLoading(true);
      setInvitationRebatesError('');
      try {
        const res = await API.get('/api/user/aff/rebates', {
          params: {
            p: requestedPage,
            page_size: PAGE_SIZE,
            _ts: Date.now(),
          },
        });
        if (requestId !== invitationRebatesRequestRef.current) {
          return;
        }
        const { success, message, data } = res.data;
        if (!success) {
          setInvitationRebates([]);
          setInvitationRebatesTotal(0);
          setInvitationRebatesError(message || t('加载返佣明细失败'));
          return;
        }
        setInvitationRebates(data?.items || []);
        setInvitationRebatesTotal(Number(data?.total) || 0);
      } catch (error) {
        if (requestId !== invitationRebatesRequestRef.current) {
          return;
        }
        setInvitationRebates([]);
        setInvitationRebatesTotal(0);
        setInvitationRebatesError(
          error?.response?.data?.message ||
            error?.message ||
            t('加载返佣明细失败'),
        );
      } finally {
        if (requestId === invitationRebatesRequestRef.current) {
          setInvitationRebatesLoading(false);
        }
      }
    },
    [t],
  );

  const transfer = async () => {
    const transferQuota = displayAmountToQuota(transferAmount);
    if (transferQuota < getQuotaPerUnit()) {
      showError(t('划转金额最低为') + ' ' + renderQuota(getQuotaPerUnit()));
      return;
    }
    try {
      const res = await API.post('/api/user/aff_transfer', {
        quota: transferQuota,
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(message);
        setOpenTransfer(false);
        await getUserQuota();
      } else {
        showError(message);
      }
    } catch (error) {
      showError(
        error?.response?.data?.message || error?.message || t('划转失败'),
      );
    }
  };

  const handleAffLinkClick = async () => {
    await copy(affLink);
    showSuccess(t('邀请链接已复制到剪切板'));
  };

  useEffect(() => {
    setTransferAmount(minTransferAmount);
    void getUserQuota();
    void getPaymentComplianceStatus();
  }, [getPaymentComplianceStatus, getUserQuota, minTransferAmount]);

  useEffect(() => {
    void getInvitationRecords(invitationRecordsPage);
  }, [getInvitationRecords, invitationRecordsPage]);

  useEffect(() => {
    void getInvitationRebates(invitationRebatesPage);
  }, [getInvitationRebates, invitationRebatesPage]);

  useEffect(() => {
    if (affFetchedRef.current) return;
    affFetchedRef.current = true;
    void getAffLink();
  }, [getAffLink]);

  let complianceMessage = '';
  if (complianceState === 'loading') {
    complianceMessage = t('正在确认邀请奖励可用状态，划转暂不可用。');
  } else if (complianceState === 'blocked') {
    complianceMessage = t('邀请奖励划转已禁用，管理员需先确认合规声明。');
  } else if (complianceState === 'error') {
    complianceMessage = t('无法确认邀请奖励可用状态，请稍后重试。');
  }

  return (
    <div className={className}>
      <TransferModal
        t={t}
        openTransfer={openTransfer}
        transfer={transfer}
        handleTransferCancel={() => setOpenTransfer(false)}
        userState={userState}
        renderQuota={renderQuota}
        getQuotaPerUnit={getQuotaPerUnit}
        minTransferAmount={minTransferAmount}
        maxTransferAmount={quotaToDisplayAmount(
          userState?.user?.aff_quota || 0,
        )}
        quotaDisplayType={getCurrencyConfig().type}
        transferAmount={transferAmount}
        setTransferAmount={setTransferAmount}
      />
      <InvitationCard
        t={t}
        userState={userState}
        renderQuota={renderQuota}
        setOpenTransfer={setOpenTransfer}
        affLink={affLink}
        handleAffLinkClick={handleAffLinkClick}
        complianceConfirmed={complianceState === 'confirmed'}
        complianceMessage={complianceMessage}
        inviteRebatePercentageText={inviteRebatePercentageText}
        inviteRewardDisplayText={inviteRewardDisplayText}
        invitationRecords={invitationRecords}
        invitationRecordsLoading={invitationRecordsLoading}
        invitationRecordsError={invitationRecordsError}
        invitationRecordsPage={invitationRecordsPage}
        invitationRecordsPageSize={PAGE_SIZE}
        invitationRecordsTotal={invitationRecordsTotal}
        onInvitationRecordsPageChange={setInvitationRecordsPage}
        onRetryInvitationRecords={() =>
          getInvitationRecords(invitationRecordsPage)
        }
        invitationRebates={invitationRebates}
        invitationRebatesLoading={invitationRebatesLoading}
        invitationRebatesError={invitationRebatesError}
        invitationRebatesPage={invitationRebatesPage}
        invitationRebatesPageSize={PAGE_SIZE}
        invitationRebatesTotal={invitationRebatesTotal}
        onInvitationRebatesPageChange={setInvitationRebatesPage}
        onRetryInvitationRebates={() =>
          getInvitationRebates(invitationRebatesPage)
        }
        formatInviteRebateAmount={(record) => {
          if (record?.cashable === false) {
            return renderQuota(record?.rebate_quota || 0);
          }
          if (!record?.rebate_currency) {
            return t('币种未记录');
          }
          return formatMinorAmount(
            record?.rebate_amount_minor,
            record.rebate_currency,
          );
        }}
        formatDateTime={(timestamp) => {
          const numeric = Number.parseInt(timestamp, 10);
          if (!Number.isFinite(numeric) || numeric <= 0) {
            return '-';
          }
          return formatDateTimeString(new Date(numeric * 1000));
        }}
      />
    </div>
  );
};

export default InvitationRewardsPanel;
