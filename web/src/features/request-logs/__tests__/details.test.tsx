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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import { api } from '@/lib/api'

import { RequestLogDetailSheet } from '../components/request-log-detail-sheet'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
  },
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

it('shows the full conversation in aligned chat bubbles and keeps search working', async () => {
  vi.mocked(api.get).mockResolvedValue({
    data: {
      success: true,
      message: '',
      data: {
        id: 1,
        request_id: 'req-1',
        user_id: 7,
        username: 'alice',
        token_id: 9,
        token_name: 'cursor',
        protocol: 'chat_completions',
        model_name: 'gpt-5',
        status: 'success',
        http_status: 200,
        is_stream: true,
        attempt_count: 1,
        capture_status: 'success',
        record_truncated: false,
        partial: false,
        created_at: 1789950000,
        duration_ms: 123,
        expires_at: 1805502000,
        schema_version: 1,
        history_complete: true,
        channel_ids: ['2'],
        tool_names: ['lookup'],
        conversation: [
          {
            role: 'user',
            source: 'request',
            parts: [{ type: 'text', text: 'historical question' }],
          },
          {
            role: 'assistant',
            source: 'response',
            choice_index: 0,
            parts: [
              { type: 'tool_call', name: 'lookup' },
              { type: 'text', text: 'current answer' },
            ],
          },
        ],
      },
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
        <RequestLogDetailSheet requestLogId={1} open onOpenChange={vi.fn()} />
      </I18nextProvider>
    </QueryClientProvider>
  )

  const dialog = await screen.findByRole('dialog', {
    name: 'Request log details',
  })
  expect(await within(dialog).findByText('Tool call: lookup')).toBeVisible()
  const messages = dialog.querySelectorAll('details')
  expect(messages).toHaveLength(2)
  expect(messages[0].parentElement).toHaveClass('is-user', 'justify-end')
  expect(messages[1].parentElement).toHaveClass(
    'is-assistant',
    'flex-row-reverse',
    'justify-end'
  )
  expect(messages[0]).toHaveAttribute('open')
  expect(messages[1]).toHaveAttribute('open')
  expect(within(dialog).getByText('historical question')).toBeVisible()

  await userEvent.type(
    within(dialog).getByRole('textbox', { name: 'Search conversation' }),
    'current answer'
  )
  expect(
    within(dialog).queryByText('historical question')
  ).not.toBeInTheDocument()
  expect(within(dialog).getByText('current answer')).toBeVisible()
  expect(
    within(dialog).queryByRole('button', { name: 'Expand' })
  ).not.toBeInTheDocument()
})

function longMessageDetail(
  content: string | Array<{ type: 'text'; text: string }>
) {
  const parts =
    typeof content === 'string' ? [{ type: 'text', text: content }] : content
  return {
    data: {
      success: true,
      message: '',
      data: {
        id: 2,
        request_id: 'req-2',
        user_id: 7,
        username: 'alice',
        token_id: 9,
        token_name: 'cursor',
        protocol: 'chat_completions',
        model_name: 'gpt-5',
        status: 'success',
        http_status: 200,
        is_stream: true,
        attempt_count: 1,
        capture_status: 'success',
        record_truncated: false,
        partial: false,
        created_at: 1789950000,
        duration_ms: 123,
        expires_at: 1805502000,
        schema_version: 1,
        history_complete: true,
        channel_ids: ['2'],
        tool_names: [],
        conversation: [
          {
            role: 'assistant',
            source: 'response',
            choice_index: 0,
            parts,
          },
        ],
      },
    },
  }
}

async function renderDetailSheet() {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: { en } })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <RequestLogDetailSheet requestLogId={2} open onOpenChange={vi.fn()} />
      </I18nextProvider>
    </QueryClientProvider>
  )
  const dialog = await screen.findByRole('dialog', {
    name: 'Request log details',
  })
  await within(dialog).findByRole('textbox', { name: 'Search conversation' })
  return dialog
}

it('collapses long message text until Expand is clicked', async () => {
  const longAnswer = `VISIBLE_HEAD ${'x'.repeat(200)} UNIQUE_TAIL`
  vi.mocked(api.get).mockResolvedValue(longMessageDetail(longAnswer))

  const dialog = await renderDetailSheet()
  expect(within(dialog).getByText(/VISIBLE_HEAD/)).toBeVisible()
  expect(within(dialog).queryByText(/UNIQUE_TAIL/)).not.toBeInTheDocument()

  const expand = within(dialog).getByRole('button', { name: 'Expand' })
  expect(expand).toHaveAttribute('aria-expanded', 'false')
  await userEvent.click(expand)
  expect(within(dialog).getByText(/UNIQUE_TAIL/)).toBeVisible()

  const collapse = within(dialog).getByRole('button', { name: 'Collapse' })
  expect(collapse).toHaveAttribute('aria-expanded', 'true')
  await userEvent.click(collapse)
  expect(within(dialog).queryByText(/UNIQUE_TAIL/)).not.toBeInTheDocument()
})

it('collapses consecutive text parts in one message with a single Expand control', async () => {
  vi.mocked(api.get).mockResolvedValue(
    longMessageDetail([
      { type: 'text', text: `FIRST_BLOCK ${'a'.repeat(80)}` },
      { type: 'text', text: `SECOND_BLOCK ${'b'.repeat(80)}` },
      { type: 'text', text: 'THIRD_BLOCK UNIQUE_TAIL' },
    ])
  )

  const dialog = await renderDetailSheet()
  expect(within(dialog).getByText(/FIRST_BLOCK/)).toBeVisible()
  expect(within(dialog).queryByText(/UNIQUE_TAIL/)).not.toBeInTheDocument()
  expect(
    within(dialog).getAllByRole('button', { name: 'Expand' })
  ).toHaveLength(1)

  await userEvent.click(within(dialog).getByRole('button', { name: 'Expand' }))
  expect(within(dialog).getByText(/SECOND_BLOCK/)).toBeVisible()
  expect(within(dialog).getByText(/UNIQUE_TAIL/)).toBeVisible()
})

it('reveals a collapsed long message when conversation search is used', async () => {
  const longAnswer = `VISIBLE_HEAD ${'x'.repeat(200)} UNIQUE_TAIL`
  vi.mocked(api.get).mockResolvedValue(longMessageDetail(longAnswer))

  const dialog = await renderDetailSheet()
  expect(within(dialog).queryByText(/UNIQUE_TAIL/)).not.toBeInTheDocument()

  await userEvent.type(
    within(dialog).getByRole('textbox', { name: 'Search conversation' }),
    'UNIQUE_TAIL'
  )
  expect(within(dialog).getByText(/UNIQUE_TAIL/)).toBeVisible()
  expect(
    within(dialog).queryByRole('button', { name: 'Expand' })
  ).not.toBeInTheDocument()
})
