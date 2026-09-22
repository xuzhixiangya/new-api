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
import { afterEach, expect, it, vi } from 'vitest'

import {
  buildApiParams,
  getDefaultTimeRange,
  getDefaultTimeRangeUnix,
} from '../utils'

afterEach(() => {
  vi.useRealTimers()
})

it('defaults to today ending at the end of the day', () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 8, 22, 12, 30, 0))

  expect(getDefaultTimeRange()).toEqual({
    start: new Date(2026, 8, 22, 0, 0, 0, 0),
    end: new Date(2026, 8, 22, 23, 59, 59, 999),
  })
  expect(getDefaultTimeRangeUnix()).toEqual({
    startTimestamp: Math.floor(new Date(2026, 8, 22).getTime() / 1000),
    endTimestamp: Math.floor(
      new Date(2026, 8, 22, 23, 59, 59, 999).getTime() / 1000
    ),
  })
})

it('uses today when the log query has no time filters', () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 8, 22, 12, 30, 0))

  expect(
    buildApiParams({
      page: 1,
      pageSize: 20,
      searchParams: {},
      isAdmin: false,
    })
  ).toEqual({
    p: 1,
    page_size: 20,
    start_timestamp: Math.floor(new Date(2026, 8, 22).getTime() / 1000),
    end_timestamp: Math.floor(
      new Date(2026, 8, 22, 23, 59, 59, 999).getTime() / 1000
    ),
  })
})

it('keeps an explicit log time range instead of the default', () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 8, 22, 12, 30, 0))

  expect(
    buildApiParams({
      page: 2,
      pageSize: 50,
      searchParams: {
        startTime: new Date(2026, 8, 21, 8, 0, 0).getTime(),
        endTime: new Date(2026, 8, 21, 18, 0, 0).getTime(),
      },
      isAdmin: false,
    })
  ).toEqual({
    p: 2,
    page_size: 50,
    start_timestamp: Math.floor(new Date(2026, 8, 21, 8, 0, 0).getTime() / 1000),
    end_timestamp: Math.floor(new Date(2026, 8, 21, 18, 0, 0).getTime() / 1000),
  })
})
