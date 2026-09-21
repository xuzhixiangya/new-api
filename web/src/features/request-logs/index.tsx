import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'

import { RequestLogsTable } from './components/request-logs-table'

export function RequestLogs() {
  const { t } = useTranslation()

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Request Logs')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <RequestLogsTable />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
