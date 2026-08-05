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

import { Affiliate } from '@/features/affiliate'
import type { AffiliateSearchState } from '@/features/affiliate/types'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

const affiliateSearchSchema = z.object({
  page: z.coerce.number().int().min(1).catch(1),
  pageSize: z.coerce.number().int().min(1).max(100).catch(10),
  keyword: z.string().catch(''),
  tab: z.enum(['relations', 'commissions']).catch('relations'),
})

export const Route = createFileRoute('/_authenticated/affiliate/')({
  beforeLoad: () => {
    const user = useAuthStore.getState().auth.user
    if (!user || user.role < ROLE.ADMIN) throw redirect({ to: '/403' })
  },
  validateSearch: affiliateSearchSchema,
  component: AffiliateRoute,
})

function AffiliateRoute() {
  const search = affiliateSearchSchema.parse(Route.useSearch())
  const navigate = Route.useNavigate()
  return (
    <Affiliate
      search={search}
      onSearchChange={(next: AffiliateSearchState) => {
        void navigate({ search: next, replace: true } as never)
      }}
    />
  )
}
