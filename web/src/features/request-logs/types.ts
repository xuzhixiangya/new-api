export type RequestLogPartType =
  | 'text'
  | 'tool_call'
  | 'tool_result'
  | 'image'
  | 'file'

export type RequestLogPart = {
  type: RequestLogPartType
  text?: string
  name?: string
}

export type RequestLogMessage = {
  role: string
  original_role?: string
  parts: RequestLogPart[]
  source: 'request' | 'response'
  choice_index?: number
  partial?: boolean
  truncated?: boolean
}

export type RequestLogListItem = {
  id: number
  request_id: string
  user_id: number
  username: string
  token_id: number
  token_name: string
  protocol: string
  model_name: string
  response_model?: string
  status: string
  http_status: number
  is_stream: boolean
  attempt_count: number
  capture_status: string
  record_truncated: boolean
  partial: boolean
  error_code?: string
  created_at: number
  duration_ms: number
}

export type RequestLogDetail = RequestLogListItem & {
  channel_ids: string[]
  conversation: RequestLogMessage[]
  tool_names: string[]
  history_complete: boolean
  history_reference?: string
  schema_version: number
  expires_at: number
}

export type RequestLogFilters = {
  userId?: number
  tokenId?: number
  username?: string
  tokenName?: string
  model?: string
  protocol?: string
  status?: string
  requestId?: string
  startTimestamp?: number
  endTimestamp?: number
}

export type RequestLogsResponse = {
  success: boolean
  message: string
  data: RequestLogListItem[]
  total: number
}

export type RequestLogDetailResponse = {
  success: boolean
  message: string
  data: RequestLogDetail
}
