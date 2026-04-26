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

import React, { useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  API,
  copy,
  formatDateTimeString,
  getCurrencyConfig,
  getQuotaPerUnit,
  renderQuota,
  setUserData,
  showError,
  showSuccess,
} from '../../helpers';
import { displayAmountToQuota, quotaToDisplayAmount } from '../../helpers/quota';
import { UserContext } from '../../context/User';
import { StatusContext } from '../../context/Status';
import InvitationCard from '../topup/InvitationCard';
import TransferModal from '../topup/modals/TransferModal';

const InvitationRewardsPanel = ({ className = '' }) => {
  const { t } = useTranslation();
  const [userState, userDispatch] = useContext(UserContext);
  const [statusState] = useContext(StatusContext);
  const [affLink, setAffLink] = useState('');
  const [openTransfer, setOpenTransfer] = useState(false);
  const [transferAmount, setTransferAmount] = useState(0);
  const [invitationRecords, setInvitationRecords] = useState([]);
  const [invitationRecordsLoading, setInvitationRecordsLoading] =
    useState(false);
  const [invitationRebates, setInvitationRebates] = useState([]);
  const [invitationRebatesLoading, setInvitationRebatesLoading] =
    useState(false);
  const affFetchedRef = useRef(false);
  const minTransferAmount = useMemo(
    () => quotaToDisplayAmount(getQuotaPerUnit()),
    [],
  );

  const inviteRebatePercentageText = useMemo(() => {
    const rawValue = statusState?.status?.invite_rebate_percentage;
    const parsedValue = Number.parseFloat(rawValue);
    const safeValue = Number.isFinite(parsedValue) ? parsedValue : 10;
    return Number.isInteger(safeValue)
      ? String(safeValue)
      : safeValue.toFixed(1).replace(/\.0$/, '');
  }, [statusState?.status?.invite_rebate_percentage]);

  const inviteRewardDisplayText = useMemo(() => {
    const quota = Number.parseFloat(statusState?.status?.quota_for_inviter);
    const safeQuota = Number.isFinite(quota) ? quota : 0;
    return renderQuota(safeQuota);
  }, [renderQuota, statusState?.status?.quota_for_inviter]);

  const getUserQuota = async () => {
    const res = await API.get('/api/user/self');
    const { success, message, data } = res.data;
    if (success) {
      userDispatch({ type: 'login', payload: data });
      setUserData(data);
    } else {
      showError(message);
    }
  };

  const getAffLink = async () => {
    const res = await API.get('/api/user/aff', {
      params: { _ts: Date.now() },
    });
    const { success, message, data } = res.data;
    if (success) {
      setAffLink(`${window.location.origin}/register?aff=${data}`);
    } else {
      showError(message);
    }
  };

  const getInvitationRecords = async () => {
    try {
      setInvitationRecordsLoading(true);
      const res = await API.get('/api/user/aff/records', {
        params: {
          page_size: 100,
          _ts: Date.now(),
        },
      });
      const { success, message, data } = res.data;
      if (success) {
        setInvitationRecords(data?.items || []);
        getUserQuota().then();
      } else {
        showError(message);
      }
    } finally {
      setInvitationRecordsLoading(false);
    }
  };

  const getInvitationRebates = async () => {
    try {
      setInvitationRebatesLoading(true);
      const res = await API.get('/api/user/aff/rebates', {
        params: {
          page_size: 100,
          _ts: Date.now(),
        },
      });
      const { success, message, data } = res.data;
      if (success) {
        setInvitationRebates(data?.items || []);
      } else {
        showError(message);
      }
    } finally {
      setInvitationRebatesLoading(false);
    }
  };

  const transfer = async () => {
    const transferQuota = displayAmountToQuota(transferAmount);
    if (transferQuota < getQuotaPerUnit()) {
      showError(t('划转金额最低为') + ' ' + renderQuota(getQuotaPerUnit()));
      return;
    }
    const res = await API.post('/api/user/aff_transfer', {
      quota: transferQuota,
    });
    const { success, message } = res.data;
    if (success) {
      showSuccess(message);
      setOpenTransfer(false);
      getUserQuota().then();
    } else {
      showError(message);
    }
  };

  const handleAffLinkClick = async () => {
    await copy(affLink);
    showSuccess(t('邀请链接已复制到剪切板'));
  };

  const handleTransferCancel = () => {
    setOpenTransfer(false);
  };

  useEffect(() => {
    getUserQuota().then();
    getInvitationRecords().then();
    getInvitationRebates().then();
    setTransferAmount(minTransferAmount);
  }, [minTransferAmount]);

  useEffect(() => {
    if (affFetchedRef.current) return;
    affFetchedRef.current = true;
    getAffLink().then();
  }, []);

  return (
    <div className={className}>
      <TransferModal
        t={t}
        openTransfer={openTransfer}
        transfer={transfer}
        handleTransferCancel={handleTransferCancel}
        userState={userState}
        renderQuota={renderQuota}
        getQuotaPerUnit={getQuotaPerUnit}
        minTransferAmount={minTransferAmount}
        maxTransferAmount={quotaToDisplayAmount(userState?.user?.aff_quota || 0)}
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
        inviteRebatePercentageText={inviteRebatePercentageText}
        inviteRewardDisplayText={inviteRewardDisplayText}
        invitationRecords={invitationRecords}
        invitationRecordsLoading={invitationRecordsLoading}
        invitationRebates={invitationRebates}
        invitationRebatesLoading={invitationRebatesLoading}
        formatInviteRebateAmount={(amount) => {
          const { symbol } = getCurrencyConfig();
          return `${symbol}${(Number.parseFloat(amount) || 0).toFixed(2)}`;
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
