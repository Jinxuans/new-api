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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import {
  getPromotionActivity,
  promotionQueryKeys,
  type PromotionActivityFilter,
} from '../api'

const PAGE_SIZE = 10

export function usePromotionActivity() {
  const [filter, setFilterState] = useState<PromotionActivityFilter>('all')
  const [page, setPage] = useState(1)
  const query = useQuery({
    queryKey: promotionQueryKeys.activity(filter, page, PAGE_SIZE),
    queryFn: () => getPromotionActivity(filter, page, PAGE_SIZE),
  })
  const totalPages = Math.max(
    1,
    Math.ceil((query.data?.total || 0) / PAGE_SIZE)
  )

  return {
    filter,
    page,
    query,
    totalPages,
    setFilter(nextFilter: PromotionActivityFilter) {
      setFilterState(nextFilter)
      setPage(1)
    },
    previousPage() {
      setPage((current) => Math.max(1, current - 1))
    },
    nextPage() {
      setPage((current) => current + 1)
    },
  }
}
