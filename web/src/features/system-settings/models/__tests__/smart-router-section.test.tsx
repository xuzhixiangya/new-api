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
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance, type i18n } from 'i18next'
import { useState, type ComponentProps } from 'react'
import { I18nextProvider } from 'react-i18next'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { getOperationsSectionNavItems } from '../../operations/section-registry.tsx'
import { getModelsSectionNavItems } from '../section-registry.tsx'
import { SmartRouterSection } from '../smart-router-section'

const defaults: ComponentProps<typeof SmartRouterSection>['defaultValues'] = {
  'smart_router_setting.enabled': false,
  'smart_router_setting.cheap_model': '',
  'smart_router_setting.mid_model': '',
  'smart_router_setting.strong_model': '',
  'smart_router_setting.classifier_model': '',
  'smart_router_setting.classifier_timeout_ms': 400,
}

const enabledModels = ['gpt-4o-mini', 'gpt-4o', 'o3']

let testI18n: i18n

function Fixture(props: { values?: Partial<typeof defaults> }) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false },
        },
      })
  )

  return (
    <I18nextProvider i18n={testI18n}>
      <QueryClientProvider client={client}>
        <div ref={setContainer} />
        <SettingsPageProvider actionsContainer={container}>
          <SmartRouterSection
            defaultValues={{ ...defaults, ...props.values }}
          />
        </SettingsPageProvider>
      </QueryClientProvider>
    </I18nextProvider>
  )
}

async function renderSettings(values?: Partial<typeof defaults>) {
  const route = createRootRoute({
    component: () => <Fixture values={values} />,
  })
  const router = createRouter({
    routeTree: route,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  render(<RouterProvider router={router} />)
}

beforeEach(async () => {
  testI18n = createInstance()
  await testI18n.init({
    lng: 'en',
    fallbackLng: 'en',
    resources: { en: { translation: {} } },
    interpolation: { escapeValue: false },
  })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/models_enabled') {
      return { data: { success: true, data: enabledModels } }
    }
    return { data: { success: true, data: [] } }
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

describe('Smart router settings', () => {
  it('is listed under models settings instead of operations', () => {
    expect(
      getModelsSectionNavItems(testI18n.t).map((item) => item.url)
    ).toContain('/system-settings/models/smart-router')
    expect(
      getOperationsSectionNavItems(testI18n.t).map((item) => item.url)
    ).not.toContain('/system-settings/operations/smart-router')
  })

  it('shows no-changes feedback when save is clicked without edits', async () => {
    const infoToast = vi.spyOn(toast, 'info')
    const user = userEvent.setup()
    await renderSettings()

    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    expect(infoToast).toHaveBeenCalledWith('No changes to save')
    expect(api.put).not.toHaveBeenCalled()
  })

  it('lets an enabled model be picked and saves the dotted option key', async () => {
    const user = userEvent.setup()
    await renderSettings()

    const cheapModel = await screen.findByRole('combobox', {
      name: 'Cheap model',
    })
    await user.click(cheapModel)
    await user.click(await screen.findByRole('option', { name: 'gpt-4o-mini' }))
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/api/option/', {
        key: 'smart_router_setting.cheap_model',
        value: 'gpt-4o-mini',
      })
    )
    expect(api.put).toHaveBeenCalledTimes(1)
    expect(api.put).not.toHaveBeenCalledWith(
      '/api/option/',
      expect.objectContaining({ key: 'smart_router_setting' })
    )
  })

  it('lists enabled models in the cheap model picker', async () => {
    await renderSettings()

    await userEvent.click(
      await screen.findByRole('combobox', { name: 'Cheap model' })
    )

    for (const model of enabledModels) {
      expect(await screen.findByRole('option', { name: model })).toBeVisible()
    }
  })
})
