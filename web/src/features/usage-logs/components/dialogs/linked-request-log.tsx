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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { getRequestLogs } from '@/features/request-logs/api'
import { RequestLogDetailSheet } from '@/features/request-logs/components/request-log-detail-sheet'
import { createServerError } from '@/lib/server-error-message'

import { DetailRow } from './log-detail-layout'

export function LinkedRequestLog(props: {
  requestId: string
  enabled: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const query = useQuery({
    queryKey: ['request-log-by-request-id', props.requestId],
    enabled: props.enabled && props.requestId !== '',
    queryFn: async () => {
      const result = await getRequestLogs(
        { requestId: props.requestId },
        0,
        1
      )
      if (!result.success) {
        throw createServerError(result, t('Failed to load request log'))
      }
      return result.data[0] ?? null
    },
  })

  let value = (
    <span className='text-muted-foreground'>
      {t('No request log was captured for this request.')}
    </span>
  )
  if (query.isLoading) {
    value = (
      <span className='text-muted-foreground'>
        {t('Loading request details...')}
      </span>
    )
  } else if (query.isError) {
    value = (
      <Button
        type='button'
        variant='ghost'
        size='xs'
        onClick={() => void query.refetch()}
      >
        {t('Retry')}
      </Button>
    )
  } else if (query.data) {
    value = (
      <Button
        type='button'
        variant='outline'
        size='xs'
        onClick={() => setOpen(true)}
      >
        {t('View request log')}
      </Button>
    )
  }

  return (
    <>
      <DetailRow label={t('Request log')} value={value} />
      <RequestLogDetailSheet
        requestLogId={query.data?.id ?? null}
        open={open && query.data != null}
        onOpenChange={setOpen}
      />
    </>
  )
}
