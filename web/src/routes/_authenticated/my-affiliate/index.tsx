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
import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'

import { MyAffiliate } from '@/features/my-affiliate'
import type { MyAffiliateSearchState } from '@/features/my-affiliate/types'
import { useAuthStore } from '@/stores/auth-store'

const myAffiliateSearchSchema = z.object({
  page: z.coerce.number().int().min(1).catch(1),
  pageSize: z.coerce.number().int().min(1).max(100).catch(10),
  keyword: z.string().catch(''),
  tab: z.enum(['invitees', 'commissions']).catch('invitees'),
})

export const Route = createFileRoute('/_authenticated/my-affiliate/')({
  beforeLoad: () => {
    if (!useAuthStore.getState().auth.user) {
      throw redirect({ to: '/403' })
    }
  },
  validateSearch: myAffiliateSearchSchema,
  component: MyAffiliateRoute,
})

function MyAffiliateRoute() {
  const search = myAffiliateSearchSchema.parse(Route.useSearch())
  const navigate = Route.useNavigate()

  return (
    <MyAffiliate
      search={search}
      onSearchChange={(next: MyAffiliateSearchState) => {
        void navigate({ search: next, replace: true } as never)
      }}
    />
  )
}
