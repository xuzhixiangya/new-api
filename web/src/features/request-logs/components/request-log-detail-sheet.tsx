import { useQuery } from '@tanstack/react-query'
import { Bot, ChevronDown, Search, UserRound } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Message } from '@/components/ai-elements/message'
import { CopyButton } from '@/components/copy-button'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  DetailRow,
  DetailSection,
} from '@/features/usage-logs/components/dialogs/log-detail-layout'
import { formatTimestamp } from '@/lib/format'
import { createServerError } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getRequestLog } from '../api'
import type {
  RequestLogMessage,
  RequestLogPart,
  RequestLogPartType,
} from '../types'

function partLabel(
  type: RequestLogPartType,
  name: string | undefined,
  t: (key: string) => string
): string {
  switch (type) {
    case 'tool_call':
      return name ? `${t('Tool call')}: ${name}` : t('Tool call')
    case 'tool_result':
      return t('Tool result')
    case 'image':
      return t('Image')
    case 'file':
      return t('File')
    default:
      return ''
  }
}

function messageText(
  message: RequestLogMessage,
  t: (key: string) => string
): string {
  return message.parts
    .map((part) =>
      part.type === 'text' ? part.text : partLabel(part.type, part.name, t)
    )
    .filter(Boolean)
    .join('\n')
}

const requestLogTextPreviewLimit = 160
const requestLogTextPreviewLines = 4

function isLongRequestLogText(text: string): boolean {
  if (text.length > requestLogTextPreviewLimit) return true
  let lineCount = 1
  for (const character of text) {
    if (character !== '\n') continue
    lineCount += 1
    if (lineCount > requestLogTextPreviewLines) return true
  }
  return false
}

function previewRequestLogText(text: string): string {
  const lines = text.split('\n')
  const clipped =
    lines.length > requestLogTextPreviewLines
      ? lines.slice(0, requestLogTextPreviewLines).join('\n')
      : text
  if (clipped.length <= requestLogTextPreviewLimit) return clipped
  return clipped.slice(0, requestLogTextPreviewLimit)
}

function ExpandableMessageText(props: {
  text: string
  forceExpanded: boolean
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const isLong = isLongRequestLogText(props.text)
  const showFull = !isLong || expanded || props.forceExpanded
  const displayText = showFull
    ? props.text
    : `${previewRequestLogText(props.text)}…`

  return (
    <div className='space-y-2'>
      <pre className='font-sans text-sm leading-6 break-words whitespace-pre-wrap'>
        {displayText}
      </pre>
      {isLong && !props.forceExpanded && (
        <Button
          type='button'
          variant='link'
          size='xs'
          className='h-auto px-0'
          aria-expanded={showFull}
          onClick={(event) => {
            event.preventDefault()
            event.stopPropagation()
            setExpanded((current) => !current)
          }}
        >
          {showFull ? t('Collapse') : t('Expand')}
        </Button>
      )}
    </div>
  )
}

function ConversationMessage(props: {
  message: RequestLogMessage
  expandLongText: boolean
}) {
  const { t } = useTranslation()
  const roleLabels: Record<string, string> = {
    user: t('User'),
    assistant: t('Assistant'),
    tool: t('Tool'),
  }
  const content = messageText(props.message, t)
  const title = roleLabels[props.message.role] ?? props.message.role
  const isUser = props.message.role === 'user'
  const bodyBlocks: Array<
    | { kind: 'text'; text: string }
    | { kind: 'other'; part: RequestLogPart }
  > = []
  for (const part of props.message.parts) {
    if (part.type === 'text') {
      const last = bodyBlocks.at(-1)
      if (last?.kind === 'text') {
        last.text += `\n\n${part.text ?? ''}`
        continue
      }
      bodyBlocks.push({ kind: 'text', text: part.text ?? '' })
      continue
    }
    bodyBlocks.push({ kind: 'other', part })
  }

  return (
    <Message
      from={isUser ? 'user' : 'assistant'}
      data-message-role={props.message.role}
      className='items-start gap-2.5 py-1'
    >
      <details
        className={cn(
          'group relative w-fit min-w-0 max-w-[82%] overflow-hidden rounded-2xl border shadow-sm transition-shadow open:shadow-md sm:max-w-[76%]',
          isUser
            ? 'border-primary/20 bg-primary/8 rounded-tr-md'
            : 'border-border/70 bg-muted/35 rounded-tl-md'
        )}
        open
      >
        <summary className='hover:bg-foreground/[0.025] flex cursor-pointer list-none flex-wrap items-center gap-x-2 gap-y-1 py-2.5 pr-20 pl-4 group-open:border-b marker:hidden'>
          <span className='text-xs font-semibold'>{title}</span>
          <span className='text-muted-foreground hidden text-[11px] sm:inline'>
            {props.message.source === 'response'
              ? t('Current response')
              : t('Request history')}
          </span>
          {props.message.choice_index !== undefined && (
            <Badge variant='secondary'>
              {t('Choice {{index}}', {
                index: props.message.choice_index + 1,
              })}
            </Badge>
          )}
          {props.message.truncated && (
            <Badge variant='destructive'>{t('Truncated')}</Badge>
          )}
          {props.message.partial && (
            <Badge variant='destructive'>{t('Partial')}</Badge>
          )}
          <ChevronDown
            aria-hidden='true'
            className='text-muted-foreground ml-auto size-3.5 transition-transform group-open:rotate-180'
          />
        </summary>
        <CopyButton
          value={content}
          className='absolute top-1.5 right-2 z-10'
          aria-label={t('Copy message')}
        />
        <div className='space-y-3 px-4 py-3.5'>
          {bodyBlocks.map((block) =>
            block.kind === 'text' ? (
              <ExpandableMessageText
                key={`text:${block.text.length}:${block.text.slice(0, 64)}`}
                text={block.text}
                forceExpanded={props.expandLongText}
              />
            ) : (
              <Badge
                key={`other:${block.part.type}:${block.part.name ?? ''}`}
                variant='secondary'
              >
                {partLabel(block.part.type, block.part.name, t)}
              </Badge>
            )
          )}
        </div>
      </details>
      <Avatar
        aria-hidden='true'
        className={cn(
          'mt-1 size-8 shadow-sm',
          isUser ? 'ring-primary/20 ring-2' : 'ring-border ring-1'
        )}
      >
        <AvatarFallback
          className={cn(
            isUser
              ? 'bg-primary text-primary-foreground'
              : 'bg-muted text-muted-foreground'
          )}
        >
          {isUser ? (
            <UserRound className='size-4' />
          ) : (
            <Bot className='size-4' />
          )}
        </AvatarFallback>
      </Avatar>
    </Message>
  )
}

export function RequestLogDetailSheet(props: {
  requestLogId: number | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const normalizedSearch = search.trim().toLocaleLowerCase()
  const query = useQuery({
    queryKey: ['request-log', props.requestLogId],
    enabled: props.open && props.requestLogId !== null,
    queryFn: async () => {
      const result = await getRequestLog(props.requestLogId as number)
      if (!result.success) {
        throw createServerError(result, t('Failed to load request log'))
      }
      return result.data
    },
  })
  const messages = useMemo(() => {
    const conversation = query.data?.conversation ?? []
    if (!normalizedSearch) return conversation
    return conversation.filter((message) =>
      messageText(message, t).toLocaleLowerCase().includes(normalizedSearch)
    )
  }, [query.data?.conversation, normalizedSearch, t])

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='z-[60] w-full sm:max-w-5xl'>
        <SheetHeader className='border-b pr-14'>
          <SheetTitle>{t('Request log details')}</SheetTitle>
          <SheetDescription>
            {query.data?.request_id ?? t('Loading request details...')}
          </SheetDescription>
        </SheetHeader>
        {query.isLoading && (
          <LoadingState message={t('Loading request details...')} />
        )}
        {!query.isLoading && (query.isError || !query.data) && (
          <ErrorState
            title={t('Failed to load request log')}
            onRetry={() => void query.refetch()}
          />
        )}
        {!query.isLoading && !query.isError && query.data && (
          <div className='flex min-h-0 flex-1 flex-col gap-4 overflow-hidden px-4 pb-4'>
            {(query.data.record_truncated || query.data.partial) && (
              <div className='flex gap-2'>
                {query.data.record_truncated && (
                  <Badge variant='destructive'>{t('Truncated')}</Badge>
                )}
                {query.data.partial && (
                  <Badge variant='destructive'>{t('Partial')}</Badge>
                )}
              </div>
            )}
            <DetailSection label={t('Request metadata')}>
              <DetailRow
                label={t('Time')}
                value={formatTimestamp(query.data.created_at)}
              />
              <DetailRow
                label={t('User')}
                value={`${query.data.username || '-'} (#${query.data.user_id})`}
              />
              <DetailRow
                label={t('API key')}
                value={`${query.data.token_name || '-'} (#${query.data.token_id})`}
              />
              <DetailRow
                label={t('Model')}
                value={query.data.model_name}
                mono
              />
              <DetailRow
                label={t('Protocol')}
                value={query.data.protocol}
                mono
              />
              <DetailRow
                label={t('Duration')}
                value={t('{{count}} ms', { count: query.data.duration_ms })}
              />
              {!query.data.history_complete && (
                <DetailRow
                  label={t('External history')}
                  value={
                    query.data.history_reference ||
                    t('Referenced but not included')
                  }
                  mono
                />
              )}
            </DetailSection>
            <div className='relative'>
              <Search
                aria-hidden='true'
                className='text-muted-foreground absolute top-2.5 left-3 size-4'
              />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('Search conversation')}
                aria-label={t('Search conversation')}
                className='pl-9'
              />
            </div>
            <div className='bg-muted/15 min-h-0 flex-1 space-y-2 overflow-y-auto rounded-xl border px-3 py-4 sm:px-5'>
              {messages.length > 0 ? (
                messages.map((message) => (
                  <ConversationMessage
                    key={`${message.source}:${message.role}:${message.choice_index ?? 'history'}:${message.parts.length}:${message.parts[0]?.text?.slice(0, 64) ?? message.parts[0]?.type ?? ''}`}
                    message={message}
                    expandLongText={normalizedSearch !== ''}
                  />
                ))
              ) : (
                <p className='text-muted-foreground py-12 text-center text-sm'>
                  {t('No conversation messages match this search.')}
                </p>
              )}
            </div>
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
