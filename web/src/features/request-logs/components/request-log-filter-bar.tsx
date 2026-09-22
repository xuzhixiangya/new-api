import type { Table } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { Combobox } from '@/components/ui/combobox'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { getDefaultTimeRangeUnix } from '@/features/usage-logs/lib'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '@/features/usage-logs/components/logs-filter-toolbar'

import type { RequestLogFilters, RequestLogListItem } from '../types'

function RequestLogFilterSelect(props: {
  label: string
  value: string
  options: Array<{ value: string; label: string }>
  onChange: (value: string) => void
}) {
  return (
    <LogsFilterField>
      <Combobox
        options={props.options}
        value={props.value}
        onValueChange={(value) => {
          if (value !== null) props.onChange(value)
        }}
        aria-label={props.label}
        className='w-full'
      />
    </LogsFilterField>
  )
}

export function RequestLogFilterBar(props: {
  table: Table<RequestLogListItem>
  filters: RequestLogFilters
  onChange: (patch: Partial<RequestLogFilters>) => void
  isFetching: boolean
  onSearch: () => void
  onReset: () => void
}) {
  const { t } = useTranslation()
  const dateFilter = (
    <LogsFilterField wide>
      <CompactDateTimeRangePicker
        start={
          props.filters.startTimestamp === undefined
            ? undefined
            : new Date(props.filters.startTimestamp * 1000)
        }
        end={
          props.filters.endTimestamp === undefined
            ? undefined
            : new Date(props.filters.endTimestamp * 1000)
        }
        onChange={({ start, end }) =>
          props.onChange({
            startTimestamp: start
              ? Math.floor(start.getTime() / 1000)
              : undefined,
            endTimestamp: end ? Math.floor(end.getTime() / 1000) : undefined,
          })
        }
      />
    </LogsFilterField>
  )
  const primaryFilters = (
    <>
      <RequestLogFilterSelect
        label={t('Protocol')}
        value={props.filters.protocol ?? 'all'}
        options={[
          { value: 'all', label: t('All protocols') },
          { value: 'chat_completions', label: 'Chat Completions' },
          { value: 'responses', label: 'Responses' },
          { value: 'claude', label: 'Claude' },
          { value: 'gemini', label: 'Gemini' },
        ]}
        onChange={(value) =>
          props.onChange({ protocol: value === 'all' ? undefined : value })
        }
      />
      <RequestLogFilterSelect
        label={t('Status')}
        value={props.filters.status ?? 'all'}
        options={[
          { value: 'all', label: t('All statuses') },
          { value: 'success', label: t('Success') },
          { value: 'failed', label: t('Failed') },
          { value: 'incomplete', label: t('Incomplete') },
          { value: 'client_disconnected', label: t('Client disconnected') },
          { value: 'timeout', label: t('Timeout') },
        ]}
        onChange={(value) =>
          props.onChange({ status: value === 'all' ? undefined : value })
        }
      />
    </>
  )
  const advancedFilters = (
    <>
      <LogsFilterField>
        <LogsFilterInput
          type='number'
          min={1}
          aria-label={t('User ID')}
          placeholder={t('User ID')}
          value={props.filters.userId ?? ''}
          onChange={(event) =>
            props.onChange({
              userId: event.target.value
                ? event.target.valueAsNumber
                : undefined,
            })
          }
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          aria-label={t('Username')}
          placeholder={t('Username')}
          value={props.filters.username ?? ''}
          onChange={(event) =>
            props.onChange({ username: event.target.value || undefined })
          }
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          type='number'
          min={1}
          aria-label={t('Token ID')}
          placeholder={t('Token ID')}
          value={props.filters.tokenId ?? ''}
          onChange={(event) =>
            props.onChange({
              tokenId: event.target.value
                ? event.target.valueAsNumber
                : undefined,
            })
          }
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          aria-label={t('API key name')}
          placeholder={t('API key name')}
          value={props.filters.tokenName ?? ''}
          onChange={(event) =>
            props.onChange({ tokenName: event.target.value || undefined })
          }
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          aria-label={t('Model')}
          placeholder={t('Model')}
          value={props.filters.model ?? ''}
          onChange={(event) =>
            props.onChange({ model: event.target.value || undefined })
          }
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          aria-label={t('Request ID')}
          placeholder={t('Request ID')}
          value={props.filters.requestId ?? ''}
          onChange={(event) =>
            props.onChange({ requestId: event.target.value || undefined })
          }
        />
      </LogsFilterField>
    </>
  )
  const advancedCount = [
    props.filters.userId,
    props.filters.username,
    props.filters.tokenId,
    props.filters.tokenName,
    props.filters.model,
    props.filters.requestId,
  ].filter(Boolean).length
  const filterCount =
    advancedCount +
    [props.filters.protocol, props.filters.status].filter(Boolean).length
  const defaultTimeRange = getDefaultTimeRangeUnix()
  const hasCustomDateRange =
    props.filters.startTimestamp !== defaultTimeRange.startTimestamp ||
    props.filters.endTimestamp !== defaultTimeRange.endTimestamp
  const hasFilters = filterCount > 0 || hasCustomDateRange

  return (
    <LogsFilterToolbar
      table={props.table}
      primaryFilters={
        <>
          {dateFilter}
          {primaryFilters}
        </>
      }
      advancedFilters={advancedFilters}
      mobilePinnedFilters={dateFilter}
      mobileFilters={
        <>
          {primaryFilters}
          {advancedFilters}
        </>
      }
      mobileFilterCount={filterCount}
      advancedFilterCount={advancedCount}
      hasActiveFilters={hasFilters}
      hasAdvancedActiveFilters={advancedCount > 0}
      searchLoading={props.isFetching}
      onSearch={props.onSearch}
      onReset={props.onReset}
    />
  )
}
