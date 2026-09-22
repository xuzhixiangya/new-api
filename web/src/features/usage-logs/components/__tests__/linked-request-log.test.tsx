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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { UsageLog } from '../../data/schema'
import { DetailsDialog } from '../dialogs/details-dialog'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
  },
}))

const queryClients: QueryClient[] = []

function makeLog(): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 2,
    content: '',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-5',
    quota: 1,
    prompt_tokens: 1,
    completion_tokens: 1,
    use_time: 1,
    is_stream: false,
    channel: 1,
    channel_name: 'channel',
    token_id: 1,
    group: 'default',
    ip: '',
    other: '{}',
    request_id: 'req-linked',
    upstream_request_id: '',
  }
}

function renderDetails(isAdmin: boolean): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const freshAt = Date.now() + 60_000
  queryClient.setQueryData(['status'], {}, { updatedAt: freshAt })
  queryClients.push(queryClient)
  render(
    <QueryClientProvider client={queryClient}>
      <DetailsDialog
        log={makeLog()}
        isAdmin={isAdmin}
        isRoot={false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  for (const queryClient of queryClients) {
    queryClient.clear()
  }
  queryClients.length = 0
  vi.clearAllMocks()
})

test('opens the captured request log from an admin usage detail', async () => {
  vi.mocked(api.get).mockImplementation(async (url: string) => {
    if (url.startsWith('/api/request_logs/?')) {
      return {
        data: {
          success: true,
          message: '',
          total: 1,
          data: [{ id: 42, request_id: 'req-linked' }],
        },
      }
    }
    return {
      data: {
        success: true,
        message: '',
        data: {
          id: 42,
          request_id: 'req-linked',
          user_id: 1,
          username: 'user',
          token_id: 1,
          token_name: 'token',
          protocol: 'chat_completions',
          model_name: 'gpt-5',
          status: 'success',
          http_status: 200,
          is_stream: false,
          attempt_count: 1,
          capture_status: 'success',
          record_truncated: false,
          partial: false,
          created_at: 1,
          duration_ms: 10,
          expires_at: 2,
          schema_version: 1,
          history_complete: true,
          channel_ids: [],
          tool_names: [],
          conversation: [
            {
              role: 'user',
              source: 'request',
              parts: [{ type: 'text', text: 'hello from the call' }],
            },
          ],
        },
      },
    }
  })

  renderDetails(true)

  await userEvent.click(
    await screen.findByRole('button', { name: 'View request log' })
  )

  expect(
    await screen.findByRole('heading', { name: 'Request log details' })
  ).toBeInTheDocument()
  expect(await screen.findByText('hello from the call')).toBeInTheDocument()
})

test('says when this request has no captured request log', async () => {
  vi.mocked(api.get).mockResolvedValue({
    data: { success: true, message: '', total: 0, data: [] },
  })

  renderDetails(true)

  expect(
    await screen.findByText('No request log was captured for this request.')
  ).toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'View request log' })
  ).not.toBeInTheDocument()
})

test('does not look up request logs for a non-admin usage detail', () => {
  renderDetails(false)

  expect(screen.queryByText('Request log')).not.toBeInTheDocument()
  expect(api.get).not.toHaveBeenCalled()
})
