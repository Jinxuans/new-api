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
import type { TFunction } from 'i18next'

import {
  rewardItemCopy,
  type GrowthRewardItem,
} from '@/features/promotion/shared'
import { formatQuota } from '@/lib/format'

export function getRewardTitle(item: GrowthRewardItem, t: TFunction) {
  return t(rewardItemCopy[item.code]?.title || item.title)
}

export function getRewardDescription(item: GrowthRewardItem, t: TFunction) {
  return t(rewardItemCopy[item.code]?.description || item.description)
}

export function getRewardAmount(item: GrowthRewardItem) {
  const minimum = item.reward_quota_min ?? item.reward_quota ?? 0
  const maximum = item.reward_quota_max ?? minimum
  if (maximum > minimum) {
    return `${formatQuota(minimum)} – ${formatQuota(maximum)}`
  }
  return formatQuota(minimum)
}
