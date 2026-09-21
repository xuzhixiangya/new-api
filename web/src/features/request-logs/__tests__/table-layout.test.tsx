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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import { api } from '@/lib/api'

import { RequestLogsTable } from '../components/request-logs-table'

const navigate = vi.hoisted(() => vi.fn())

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    getRouteApi: () => ({
      useSearch: () => ({ page: 1, pageSize: 50 }),
      useNavigate: () => navigate,
    }),
  }
})

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
  },
}))

vi.mock('../components/request-log-filter-bar', () => ({
  RequestLogFilterBar: (props: { onSearch: () => void }) => (
    <button type='button' onClick={props.onSearch}>
      Search
    </button>
  ),
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

it('renders request records with the shared user, token, model, and status visuals', async () => {
  vi.mocked(api.get).mockResolvedValue({
    data: {
      success: true,
      message: '',
      total: 1,
      data: [
        {
          id: 1,
          request_id: 'req-1',
          user_id: 7,
          username: 'alice',
          token_id: 9,
          token_name: 'playground-default',
          protocol: 'chat_completions',
          model_name: 'openai/gpt-5.4',
          status: 'success',
          http_status: 200,
          is_stream: false,
          attempt_count: 1,
          capture_status: 'success',
          record_truncated: false,
          partial: false,
          created_at: 1789950000,
          duration_ms: 1500,
        },
      ],
    },
  })
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: { en } })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <RequestLogsTable />
      </I18nextProvider>
    </QueryClientProvider>
  )

  expect(await screen.findByText('alice')).toBeVisible()
  expect(screen.getByText('playground-default')).toBeVisible()
  expect(screen.getByText('openai/gpt-5.4')).toBeVisible()
  expect(screen.getByText('Chat Completions')).toBeVisible()
  expect(screen.getByText('Success')).toBeVisible()
  expect(screen.getByText('1.5s')).toBeVisible()
})

it('refetches request logs when Search is clicked with the same filters', async () => {
  const payload = {
    data: {
      success: true,
      message: '',
      total: 1,
      data: [
        {
          id: 1,
          request_id: 'req-1',
          user_id: 7,
          username: 'alice',
          token_id: 9,
          token_name: 'playground-default',
          protocol: 'chat_completions',
          model_name: 'openai/gpt-5.4',
          status: 'success',
          http_status: 200,
          is_stream: false,
          attempt_count: 1,
          capture_status: 'success',
          record_truncated: false,
          partial: false,
          created_at: 1789950000,
          duration_ms: 1500,
        },
      ],
    },
  }
  vi.mocked(api.get).mockResolvedValue(payload)
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: { en } })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })

  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <RequestLogsTable />
      </I18nextProvider>
    </QueryClientProvider>
  )

  expect(await screen.findByText('alice')).toBeVisible()
  expect(api.get).toHaveBeenCalledTimes(1)

  await userEvent.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() => expect(api.get).toHaveBeenCalledTimes(2))
})
