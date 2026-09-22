import { describe, expect, it } from 'vitest'

import { ROLE } from '@/lib/roles'

import type { User } from '../../types'
import {
  transformFormDataToPayload,
  transformUserToFormDefaults,
  USER_FORM_DEFAULT_VALUES,
} from '../user-form'

const baseUser: User = {
  id: 7,
  username: 'alice',
  display_name: 'Alice',
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'default',
  status: 1,
  role: ROLE.USER,
}

describe('user form smart router setting', () => {
  it('reads forced routing from the user setting JSON', () => {
    const values = transformUserToFormDefaults({
      ...baseUser,
      setting: JSON.stringify({ smart_router_forced: true }),
    })
    expect(values.smart_router_forced).toBe(true)
  })

  it('treats missing or invalid setting as not forced', () => {
    expect(transformUserToFormDefaults(baseUser).smart_router_forced).toBe(
      false
    )
    expect(
      transformUserToFormDefaults({ ...baseUser, setting: '{not-json' })
        .smart_router_forced
    ).toBe(false)
  })

  it('includes forced routing on update payloads and omits it when creating', () => {
    const updatePayload = transformFormDataToPayload(
      {
        ...USER_FORM_DEFAULT_VALUES,
        username: 'alice',
        smart_router_forced: true,
      },
      7
    )
    expect(updatePayload.smart_router_forced).toBe(true)

    const createPayload = transformFormDataToPayload({
      ...USER_FORM_DEFAULT_VALUES,
      username: 'alice',
      smart_router_forced: true,
    })
    expect(createPayload.smart_router_forced).toBeUndefined()
  })
})
