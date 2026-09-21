import { api } from '@/lib/api'

import type {
  RequestLogDetailResponse,
  RequestLogFilters,
  RequestLogsResponse,
} from './types'

export async function getRequestLogs(
  filters: RequestLogFilters,
  offset: number,
  limit: number
): Promise<RequestLogsResponse> {
  const params = new URLSearchParams({
    offset: String(offset),
    limit: String(limit),
  })
  const values: Array<[string, string | number | undefined]> = [
    ['user_id', filters.userId],
    ['token_id', filters.tokenId],
    ['username', filters.username],
    ['token_name', filters.tokenName],
    ['model', filters.model],
    ['protocol', filters.protocol],
    ['status', filters.status],
    ['request_id', filters.requestId],
    ['start_timestamp', filters.startTimestamp],
    ['end_timestamp', filters.endTimestamp],
  ]
  for (const [key, value] of values) {
    if (value !== undefined && value !== '') {
      params.set(key, String(value))
    }
  }
  const response = await api.get<RequestLogsResponse>(
    `/api/request_logs/?${params.toString()}`
  )
  return response.data
}

export async function getRequestLog(
  id: number
): Promise<RequestLogDetailResponse> {
  const response = await api.get<RequestLogDetailResponse>(
    `/api/request_logs/${id}`
  )
  return response.data
}
