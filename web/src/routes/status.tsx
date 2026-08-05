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
import { createFileRoute } from '@tanstack/react-router'

import { GroupProbeStatusPage } from '@/features/group-probe-status'
import i18n from '@/i18n/config'

export const Route = createFileRoute('/status')({
  component: GroupProbeStatusPage,
  head: () => ({
    meta: [
      { title: i18n.t('Group status') },
      {
        name: 'description',
        content: i18n.t('Current availability from scheduled group probes.'),
      },
    ],
  }),
})
