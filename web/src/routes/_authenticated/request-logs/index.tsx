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
import z from 'zod'

import { RequestLogs } from '@/features/request-logs'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

const requestLogsSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(50),
  username: z.string().optional().catch(''),
  userId: z.number().optional().catch(undefined),
  tokenName: z.string().optional().catch(''),
  tokenId: z.number().optional().catch(undefined),
  model: z.string().optional().catch(''),
  protocol: z.string().optional().catch(''),
  status: z.string().optional().catch(''),
  requestId: z.string().optional().catch(''),
  startTimestamp: z.number().optional().catch(undefined),
  endTimestamp: z.number().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/request-logs/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  validateSearch: requestLogsSearchSchema,
  component: RequestLogs,
})
