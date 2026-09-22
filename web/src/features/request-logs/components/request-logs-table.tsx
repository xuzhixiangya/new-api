import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'
import { Eye, KeyRound } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ModelBadge } from '@/features/usage-logs/components/model-badge'
import { getDefaultTimeRangeUnix } from '@/features/usage-logs/lib'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { getUserAvatarFallback, getUserAvatarStyle } from '@/lib/avatar'
import { formatTimestamp, formatUseTime } from '@/lib/format'
import { createServerError } from '@/lib/server-error-message'

import { getRequestLogs } from '../api'
import type { RequestLogFilters, RequestLogListItem } from '../types'
import { RequestLogDetailSheet } from './request-log-detail-sheet'
import { RequestLogFilterBar } from './request-log-filter-bar'

const route = getRouteApi('/_authenticated/request-logs/')

function statusVariant(
  status: string
): 'success' | 'danger' | 'warning' | 'neutral' {
  if (status === 'success') return 'success'
  if (status === 'failed' || status === 'timeout') return 'danger'
  if (status === 'incomplete' || status === 'client_disconnected') {
    return 'warning'
  }
  return 'neutral'
}

function requestLogStatusLabel(
  status: string,
  t: (key: string) => string
): string {
  switch (status) {
    case 'success':
      return t('Success')
    case 'failed':
      return t('Failed')
    case 'incomplete':
      return t('Incomplete')
    case 'client_disconnected':
      return t('Client disconnected')
    case 'timeout':
      return t('Timeout')
    default:
      return status
  }
}

function requestLogProtocolLabel(protocol: string): string {
  switch (protocol) {
    case 'chat_completions':
      return 'Chat Completions'
    case 'responses':
      return 'Responses'
    case 'claude':
      return 'Claude'
    case 'gemini':
      return 'Gemini'
    default:
      return protocol
  }
}

export function RequestLogsTable() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const queryClient = useQueryClient()
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const getColumnClassName = useCallback(() => 'py-2.5', [])
  const [filters, setFilters] = useState<RequestLogFilters>(() => {
    const defaultTimeRange = getDefaultTimeRangeUnix()
    return {
      userId: search.userId,
      username: search.username || undefined,
      tokenId: search.tokenId,
      tokenName: search.tokenName || undefined,
      model: search.model || undefined,
      protocol: search.protocol || undefined,
      status: search.status || undefined,
      requestId: search.requestId || undefined,
      startTimestamp: search.startTimestamp ?? defaultTimeRange.startTimestamp,
      endTimestamp: search.endTimestamp ?? defaultTimeRange.endTimestamp,
    }
  })
  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: { defaultPage: 1, defaultPageSize: 50 },
      globalFilter: { enabled: false },
      columnFilters: [],
    })
  const appliedFilters = useMemo<RequestLogFilters>(() => {
    const defaultTimeRange = getDefaultTimeRangeUnix()
    return {
      userId: search.userId,
      username: search.username || undefined,
      tokenId: search.tokenId,
      tokenName: search.tokenName || undefined,
      model: search.model || undefined,
      protocol: search.protocol || undefined,
      status: search.status || undefined,
      requestId: search.requestId || undefined,
      startTimestamp: search.startTimestamp ?? defaultTimeRange.startTimestamp,
      endTimestamp: search.endTimestamp ?? defaultTimeRange.endTimestamp,
    }
  }, [search])
  const query = useQuery({
    queryKey: [
      'request-logs',
      pagination.pageIndex,
      pagination.pageSize,
      appliedFilters,
    ],
    queryFn: async () => {
      const result = await getRequestLogs(
        appliedFilters,
        pagination.pageIndex * pagination.pageSize,
        pagination.pageSize
      )
      if (!result.success) {
        throw createServerError(result, t('Failed to load request logs'))
      }
      return result
    },
    placeholderData: (previousData) => previousData,
    refetchOnMount: 'always',
  })
  const columns = useMemo<ColumnDef<RequestLogListItem>[]>(
    () => [
      {
        accessorKey: 'created_at',
        header: t('Time'),
        cell: ({ row }) => (
          <span className='whitespace-nowrap'>
            {formatTimestamp(row.original.created_at)}
          </span>
        ),
        size: 165,
      },
      {
        id: 'user',
        header: t('User'),
        cell: ({ row }) => {
          const username = row.original.username || '-'
          return (
            <div className='flex min-w-0 items-center gap-2'>
              <Avatar className='ring-border/60 size-6 ring-1'>
                <AvatarFallback
                  className='text-[11px] font-semibold'
                  style={getUserAvatarStyle(username)}
                >
                  {getUserAvatarFallback(username)}
                </AvatarFallback>
              </Avatar>
              <div className='min-w-0'>
                <div className='max-w-32 truncate font-medium'>{username}</div>
                <div className='text-muted-foreground text-[11px] leading-none'>
                  #{row.original.user_id}
                </div>
              </div>
            </div>
          )
        },
      },
      {
        id: 'token',
        header: t('Token'),
        cell: ({ row }) => (
          <div className='flex max-w-44 flex-col gap-0.5'>
            <StatusBadge
              label={row.original.token_name || '-'}
              icon={KeyRound}
              copyText={row.original.token_name || undefined}
              size='sm'
              showDot={false}
              className='border-border/60 bg-muted/30 text-foreground h-6 max-w-full gap-1.5 overflow-hidden rounded-md border px-2 py-0.5 [font-family:var(--font-body)]'
            />
            {row.original.token_id > 0 && (
              <div className='text-muted-foreground pl-0.5 text-[11px] leading-none'>
                #{row.original.token_id}
              </div>
            )}
          </div>
        ),
      },
      {
        accessorKey: 'model_name',
        header: t('Model'),
        cell: ({ row }) => <ModelBadge modelName={row.original.model_name} />,
      },
      {
        accessorKey: 'protocol',
        header: t('Protocol'),
        cell: ({ row }) => (
          <StatusBadge
            label={requestLogProtocolLabel(row.original.protocol)}
            copyable={false}
            showDot={false}
            className='border-border/60 bg-muted/30 text-foreground rounded-md border'
          />
        ),
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        cell: ({ row }) => (
          <div className='flex flex-wrap items-center gap-1'>
            <StatusBadge
              label={requestLogStatusLabel(row.original.status, t)}
              variant={statusVariant(row.original.status)}
              copyable={false}
            />
            {row.original.partial && (
              <Badge variant='destructive'>{t('Partial')}</Badge>
            )}
          </div>
        ),
      },
      {
        accessorKey: 'duration_ms',
        header: t('Duration'),
        cell: ({ row }) => (
          <StatusBadge
            label={formatUseTime(row.original.duration_ms / 1000)}
            copyable={false}
            showDot={false}
            className='border-border/60 bg-muted/30 text-foreground rounded-md border font-mono tabular-nums'
          />
        ),
      },
      {
        accessorKey: 'request_id',
        header: t('Request ID'),
        cell: ({ row }) => (
          <div className='flex max-w-48 items-center gap-1'>
            <span className='truncate font-mono text-xs'>
              {row.original.request_id}
            </span>
            <CopyButton
              value={row.original.request_id}
              aria-label={t('Copy request ID')}
            />
          </div>
        ),
      },
      {
        id: 'actions',
        header: '',
        cell: ({ row }) => (
          <Button
            variant='ghost'
            size='icon-sm'
            aria-label={t('View request log')}
            onClick={() => {
              setSelectedId(row.original.id)
              setDetailOpen(true)
            }}
          >
            <Eye aria-hidden='true' />
          </Button>
        ),
        size: 48,
      },
    ],
    [t]
  )
  const { table } = useDataTable({
    data: query.data?.data ?? [],
    columns,
    pagination,
    enableRowSelection: false,
    manualPagination: true,
    manualFiltering: true,
    totalCount: query.data?.total ?? 0,
    onPaginationChange,
    ensurePageInRange,
  })

  const applyFilters = () => {
    void navigate({
      search: {
        page: 1,
        pageSize: pagination.pageSize,
        userId: filters.userId,
        username: filters.username || undefined,
        tokenId: filters.tokenId,
        tokenName: filters.tokenName || undefined,
        model: filters.model || undefined,
        protocol: filters.protocol || undefined,
        status: filters.status || undefined,
        requestId: filters.requestId || undefined,
        startTimestamp: filters.startTimestamp,
        endTimestamp: filters.endTimestamp,
      },
    })
    void queryClient.invalidateQueries({ queryKey: ['request-logs'] })
  }
  const resetFilters = () => {
    const defaultTimeRange = getDefaultTimeRangeUnix()
    setFilters(defaultTimeRange)
    void navigate({
      search: {
        page: 1,
        pageSize: pagination.pageSize,
        startTimestamp: defaultTimeRange.startTimestamp,
        endTimestamp: defaultTimeRange.endTimestamp,
      },
    })
    void queryClient.invalidateQueries({ queryKey: ['request-logs'] })
  }

  return (
    <>
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={query.isLoading}
        isFetching={query.isFetching}
        emptyTitle={t('No request logs found')}
        emptyDescription={t(
          'Text request conversations will appear here after they pass through the gateway.'
        )}
        hideMobile
        applyHeaderSize
        skeletonKeyPrefix='request-log-skeleton'
        tableClassName='[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
        toolbar={
          <RequestLogFilterBar
            table={table}
            filters={filters}
            onChange={(patch) =>
              setFilters((current) => ({ ...current, ...patch }))
            }
            isFetching={query.isFetching}
            onSearch={applyFilters}
            onReset={resetFilters}
          />
        }
        renderRow={(row) => (
          <DataTableRow
            key={row.id}
            row={row}
            className='transition-colors'
            getColumnClassName={getColumnClassName}
            cellRenderColumns={table.options.columns}
          />
        )}
      />
      <RequestLogDetailSheet
        requestLogId={selectedId}
        open={detailOpen}
        onOpenChange={setDetailOpen}
      />
    </>
  )
}
