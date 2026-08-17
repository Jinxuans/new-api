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
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  Modal,
  Table,
  Badge,
  Typography,
  Toast,
  Empty,
  Button,
  Input,
  TextArea,
  Tag,
} from '@douyinfe/semi-ui';
import {
  IllustrationFailure,
  IllustrationFailureDark,
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { RefreshCw } from 'lucide-react';
import { IconSearch } from '@douyinfe/semi-icons';
import {
  API,
  formatMinorAmount,
  renderQuota,
  timestamp2string,
} from '../../../helpers';
import { isAdmin } from '../../../helpers/utils';
import { useIsMobile } from '../../../hooks/common/useIsMobile';

const { Text } = Typography;

const STATUS_CONFIG = {
  success: { type: 'success', key: '成功' },
  pending: { type: 'warning', key: '待支付' },
  failed: { type: 'danger', key: '失败' },
  expired: { type: 'danger', key: '已过期' },
};

const REFUND_STATUS_CONFIG = {
  partial: { color: 'orange', key: '部分退款' },
  full: { color: 'green', key: '已全额退款' },
  disputed: { color: 'red', key: '争议退款' },
};

const PURPOSE_CONFIG = {
  api_balance: { color: 'blue', key: 'API 余额' },
  subscription: { color: 'purple', key: '订阅套餐' },
};

const PAYMENT_METHOD_MAP = {
  stripe: 'Stripe',
  creem: 'Creem',
  waffo: 'Waffo',
  waffo_pancake: 'Waffo Pancake',
  alipay: '支付宝',
  wxpay: '微信',
  balance: '账户余额',
};

const TopupHistoryModal = ({ visible, onCancel, t }) => {
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [topups, setTopups] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [searchInput, setSearchInput] = useState('');
  const [keyword, setKeyword] = useState('');
  const requestIdRef = useRef(0);
  const isMobile = useIsMobile();

  const loadTopups = useCallback(
    async (currentPage, currentPageSize, currentKeyword) => {
      const requestId = ++requestIdRef.current;
      setLoading(true);
      setLoadError('');
      try {
        const base = isAdmin() ? '/api/user/topup' : '/api/user/topup/self';
        const res = await API.get(base, {
          params: {
            p: currentPage,
            page_size: currentPageSize,
            ...(currentKeyword ? { keyword: currentKeyword } : {}),
          },
        });
        if (requestId !== requestIdRef.current) {
          return;
        }
        const { success, message, data } = res.data;
        if (!success) {
          setTopups([]);
          setTotal(0);
          setLoadError(message || t('加载账单失败'));
          return;
        }
        setTopups(data?.items || []);
        setTotal(Number(data?.total) || 0);
      } catch (error) {
        if (requestId !== requestIdRef.current) {
          return;
        }
        setTopups([]);
        setTotal(0);
        setLoadError(
          error?.response?.data?.message || error?.message || t('加载账单失败'),
        );
      } finally {
        if (requestId === requestIdRef.current) {
          setLoading(false);
        }
      }
    },
    [t],
  );

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setPage(1);
      setKeyword(searchInput.trim());
    }, 300);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  useEffect(() => {
    if (!visible) {
      requestIdRef.current += 1;
      setLoading(false);
      return undefined;
    }
    void loadTopups(page, pageSize, keyword);
    return () => {
      requestIdRef.current += 1;
    };
  }, [keyword, loadTopups, page, pageSize, visible]);

  const handleAdminComplete = async (tradeNo, reason) => {
    try {
      const res = await API.post('/api/user/topup/complete', {
        trade_no: tradeNo,
        reason,
      });
      const { success, message } = res.data;
      if (success) {
        Toast.success({ content: t('补单成功') });
        await loadTopups(page, pageSize, keyword);
        return true;
      }
      Toast.error({ content: message || t('补单失败') });
    } catch (error) {
      Toast.error({
        content:
          error?.response?.data?.message || error?.message || t('补单失败'),
      });
    }
    return false;
  };

  const confirmAdminComplete = (tradeNo) => {
    let reason = '';
    Modal.confirm({
      title: t('确认补单'),
      content: (
        <div className='flex flex-col gap-2'>
          <Text>{t('是否将该订单标记为成功并为用户入账？')}</Text>
          <Text>{t('原因：')}</Text>
          <TextArea
            maxCount={1000}
            placeholder={t('请输入备注（仅管理员可见）')}
            onChange={(value) => {
              reason = value;
            }}
          />
        </div>
      ),
      onOk: async () => {
        const normalizedReason = reason.trim();
        if (!normalizedReason) {
          Toast.error({ content: t('请输入备注（仅管理员可见）') });
          return Promise.reject(new Error('manual top-up reason is required'));
        }
        const completed = await handleAdminComplete(tradeNo, normalizedReason);
        if (!completed) {
          return Promise.reject(new Error('manual top-up completion failed'));
        }
        return undefined;
      },
    });
  };

  const userIsAdmin = useMemo(() => isAdmin(), []);

  const columns = useMemo(() => {
    const baseColumns = [
      ...(userIsAdmin
        ? [
            {
              title: t('用户ID'),
              dataIndex: 'user_id',
              key: 'user_id',
              width: 90,
              render: (userId) => <Text>{userId ?? '-'}</Text>,
            },
          ]
        : []),
      {
        title: t('订单号'),
        dataIndex: 'trade_no',
        key: 'trade_no',
        width: 220,
        render: (text) => <Text copyable>{text}</Text>,
      },
      {
        title: t('用途与入账'),
        key: 'purpose',
        width: 170,
        render: (_, record) => {
          const config = PURPOSE_CONFIG[record?.purpose] || {
            color: 'grey',
            key: record?.purpose || '未记录',
          };
          const creditedQuota = Number(record?.credited_quota);
          const creditedValue =
            Number.isFinite(creditedQuota) && creditedQuota > 0
              ? renderQuota(creditedQuota)
              : t('未记录');
          return (
            <div className='flex flex-col items-start gap-1'>
              <Tag color={config.color} shape='circle' size='small'>
                {t(config.key)}
              </Tag>
              {record?.purpose !== 'subscription' ? (
                <Text type='tertiary' className='text-xs'>
                  {t('入账额度')}: {creditedValue}
                </Text>
              ) : null}
            </div>
          );
        },
      },
      {
        title: t('实付与方式'),
        key: 'payment',
        width: 190,
        render: (_, record) => {
          const paymentMethod =
            PAYMENT_METHOD_MAP[record?.payment_method] ||
            record?.payment_method ||
            '-';
          const paymentAmount = record?.paid_currency
            ? formatMinorAmount(record?.paid_amount_minor, record.paid_currency)
            : t('币种未记录');
          return (
            <div className='flex flex-col items-start gap-1'>
              <Text strong type='danger'>
                {paymentAmount}
              </Text>
              <Text type='tertiary' className='text-xs'>
                {t(paymentMethod)}
              </Text>
              <Tag
                color={record?.paid_amount_verified ? 'green' : 'orange'}
                shape='circle'
                size='small'
              >
                {record?.paid_amount_verified
                  ? t('金额已核验')
                  : t('金额未核验')}
              </Tag>
            </div>
          );
        },
      },
      {
        title: t('状态与退款'),
        key: 'status',
        width: 220,
        render: (_, record) => {
          const statusConfig = STATUS_CONFIG[record?.status] || {
            type: 'primary',
            key: record?.status || '-',
          };
          const refundConfig = REFUND_STATUS_CONFIG[record?.refund_status];
          return (
            <div className='flex flex-col items-start gap-1'>
              <span className='flex items-center gap-2'>
                <Badge dot type={statusConfig.type} />
                <Text>{t(statusConfig.key)}</Text>
              </span>
              {refundConfig ? (
                <>
                  <Tag color={refundConfig.color} shape='circle' size='small'>
                    {t(refundConfig.key)}
                  </Tag>
                  <Text type='tertiary' className='text-xs'>
                    {t('退款金额')}:{' '}
                    {record?.paid_currency
                      ? formatMinorAmount(
                          record?.refunded_amount_minor,
                          record.paid_currency,
                        )
                      : t('币种未记录')}
                  </Text>
                  <Text type='tertiary' className='text-xs'>
                    {t('退款收回额度')}:{' '}
                    {renderQuota(record?.refunded_quota || 0)}
                  </Text>
                </>
              ) : (
                <Text type='tertiary' className='text-xs'>
                  {t('无退款')}
                </Text>
              )}
            </div>
          );
        },
      },
    ];

    if (userIsAdmin) {
      baseColumns.push({
        title: t('操作'),
        key: 'action',
        width: 90,
        render: (_, record) =>
          record.status === 'pending' ? (
            <Button
              size='small'
              type='primary'
              theme='outline'
              onClick={() => confirmAdminComplete(record.trade_no)}
            >
              {t('补单')}
            </Button>
          ) : null,
      });
    }

    baseColumns.push({
      title: t('创建时间'),
      dataIndex: 'create_time',
      key: 'create_time',
      width: 170,
      render: (time) => timestamp2string(time),
    });

    return baseColumns;
  }, [keyword, loadTopups, page, pageSize, t, userIsAdmin]);

  const emptyContent = loadError ? (
    <div className='flex flex-col items-center pb-8'>
      <Empty
        image={<IllustrationFailure style={{ width: 150, height: 150 }} />}
        darkModeImage={
          <IllustrationFailureDark style={{ width: 150, height: 150 }} />
        }
        description={loadError}
        style={{ padding: 30 }}
      />
      <Button
        type='primary'
        theme='outline'
        icon={<RefreshCw size={14} />}
        onClick={() => loadTopups(page, pageSize, keyword)}
      >
        {t('重新加载')}
      </Button>
    </div>
  ) : (
    <Empty
      image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
      darkModeImage={
        <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
      }
      description={keyword ? t('未找到匹配的充值记录') : t('暂无充值记录')}
      style={{ padding: 30 }}
    />
  );

  return (
    <Modal
      title={t('充值账单')}
      visible={visible}
      onCancel={onCancel}
      footer={null}
      size={isMobile ? 'full-width' : 'large'}
    >
      <div className='mb-3 space-y-2'>
        <Input
          prefix={<IconSearch />}
          placeholder={t('搜索订单号')}
          value={searchInput}
          onChange={setSearchInput}
          showClear
        />
        <Text type='tertiary' className='text-xs'>
          {t('仅显示最近 30 天的充值记录。')}
        </Text>
      </div>
      <Table
        columns={columns}
        dataSource={topups}
        loading={loading}
        rowKey='id'
        pagination={{
          currentPage: page,
          pageSize,
          total,
          showSizeChanger: true,
          pageSizeOpts: [10, 20, 50, 100],
          onPageChange: setPage,
          onPageSizeChange: (currentPageSize) => {
            setPageSize(currentPageSize);
            setPage(1);
          },
        }}
        scroll={{ x: userIsAdmin ? 1220 : 1040 }}
        size='small'
        empty={emptyContent}
      />
    </Modal>
  );
};

export default TopupHistoryModal;
