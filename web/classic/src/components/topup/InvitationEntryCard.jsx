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

import React from 'react';
import { Avatar, Button, Card, Typography } from '@douyinfe/semi-ui';
import { ArrowRight, Gift, Users } from 'lucide-react';
import { useNavigate } from 'react-router-dom';

const { Text } = Typography;

const InvitationEntryCard = ({ t, userState, renderQuota }) => {
  const navigate = useNavigate();

  return (
    <Card className='!rounded-2xl shadow-sm border-0 h-fit'>
      <div className='flex items-start justify-between gap-4'>
        <div className='flex min-w-0 items-start gap-3'>
          <Avatar size='small' color='green' className='shrink-0 shadow-sm'>
            <Gift size={16} />
          </Avatar>
          <div className='min-w-0'>
            <Text strong className='block text-base'>
              {t('奖励与邀请中心')}
            </Text>
            <Text type='tertiary' className='mt-1 block text-sm'>
              {t('集中查看邀请额度、现金返佣和历史记录')}
            </Text>
          </div>
        </div>
        <Button
          type='primary'
          theme='outline'
          icon={<ArrowRight size={14} />}
          iconPosition='right'
          onClick={() => navigate('/console/invite')}
          className='!rounded-lg shrink-0'
        >
          {t('查看奖励中心')}
        </Button>
      </div>

      <div className='mt-4 flex flex-wrap gap-x-8 gap-y-3 border-t border-[var(--semi-color-border)] pt-4'>
        <div>
          <Text type='tertiary' className='block text-xs'>
            {t('待划转邀请额度')}
          </Text>
          <Text strong className='mt-1 block'>
            {renderQuota(userState?.user?.aff_quota || 0)}
          </Text>
        </div>
        <div>
          <Text type='tertiary' className='flex items-center gap-1 text-xs'>
            <Users size={13} />
            {t('邀请人数')}
          </Text>
          <Text strong className='mt-1 block'>
            {userState?.user?.aff_count || 0}
          </Text>
        </div>
      </div>
    </Card>
  );
};

export default InvitationEntryCard;
