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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Combobox } from '@/components/ui/combobox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { getEnabledModels } from '@/features/channels/api'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

const smartRouterSchema = z.object({
  smart_router_setting: z.object({
    enabled: z.boolean(),
    cheap_model: z.string(),
    mid_model: z.string(),
    strong_model: z.string(),
    classifier_model: z.string(),
    classifier_timeout_ms: z.number().min(100).max(5000),
  }),
})

type SmartRouterFormInput = z.input<typeof smartRouterSchema>
type SmartRouterFormValues = z.output<typeof smartRouterSchema>

type FlatSmartRouterDefaults = {
  'smart_router_setting.enabled': boolean
  'smart_router_setting.cheap_model': string
  'smart_router_setting.mid_model': string
  'smart_router_setting.strong_model': string
  'smart_router_setting.classifier_model': string
  'smart_router_setting.classifier_timeout_ms': number
}

type SmartRouterSectionProps = {
  defaultValues: FlatSmartRouterDefaults
}

const MODEL_FIELDS = [
  {
    name: 'cheap_model',
    labelKey: 'Cheap model',
    placeholderKey: 'Model for simple requests',
  },
  {
    name: 'mid_model',
    labelKey: 'Mid model',
    placeholderKey: 'Model for medium requests',
  },
  {
    name: 'strong_model',
    labelKey: 'Strong model',
    placeholderKey: 'Model for complex requests',
  },
  {
    name: 'classifier_model',
    labelKey: 'Classifier model',
    placeholderKey: 'Optional. Defaults to the cheap model.',
    descriptionKey:
      'Used only when local scoring is unsure. Leave empty to reuse the cheap model.',
  },
] as const

const buildFormDefaults = (
  defaults: FlatSmartRouterDefaults
): SmartRouterFormInput => ({
  smart_router_setting: {
    enabled: defaults['smart_router_setting.enabled'],
    cheap_model: defaults['smart_router_setting.cheap_model'],
    mid_model: defaults['smart_router_setting.mid_model'],
    strong_model: defaults['smart_router_setting.strong_model'],
    classifier_model: defaults['smart_router_setting.classifier_model'],
    classifier_timeout_ms:
      defaults['smart_router_setting.classifier_timeout_ms'],
  },
})

const normalizeFormValues = (
  values: SmartRouterFormValues
): FlatSmartRouterDefaults => ({
  'smart_router_setting.enabled': values.smart_router_setting.enabled,
  'smart_router_setting.cheap_model': values.smart_router_setting.cheap_model,
  'smart_router_setting.mid_model': values.smart_router_setting.mid_model,
  'smart_router_setting.strong_model': values.smart_router_setting.strong_model,
  'smart_router_setting.classifier_model':
    values.smart_router_setting.classifier_model,
  'smart_router_setting.classifier_timeout_ms':
    values.smart_router_setting.classifier_timeout_ms,
})

export function SmartRouterSection(props: SmartRouterSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<SmartRouterFormInput, unknown, SmartRouterFormValues>({
    resolver: zodResolver(smartRouterSchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatSmartRouterDefaults>(props.defaultValues)
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(props.defaultValues)
  )

  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = props.defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(props.defaultValues))
  }, [props.defaultValues, form])

  const enabledModelsQuery = useQuery({
    queryKey: ['enabled-models'],
    queryFn: async () => requireServerSuccess(await getEnabledModels()),
  })

  const enabledModelsError =
    enabledModelsQuery.isError ||
    (enabledModelsQuery.data !== undefined && !enabledModelsQuery.data.success)

  useEffect(() => {
    if (!enabledModelsError) return
    handleServerError(
      enabledModelsQuery.error ?? enabledModelsQuery.data,
      t('Failed to load enabled models')
    )
  }, [enabledModelsError, enabledModelsQuery.data, enabledModelsQuery.error, t])

  const currentModels = form.watch('smart_router_setting')
  const modelOptions = useMemo(() => {
    const names = new Set(enabledModelsQuery.data?.data ?? [])
    for (const name of [
      currentModels.cheap_model,
      currentModels.mid_model,
      currentModels.strong_model,
      currentModels.classifier_model,
    ]) {
      const trimmed = name.trim()
      if (trimmed) names.add(trimmed)
    }
    return [...names]
      .sort((left, right) => left.localeCompare(right))
      .map((name) => ({ value: name, label: name }))
  }, [currentModels, enabledModelsQuery.data?.data])

  const onSubmit = async (values: SmartRouterFormValues) => {
    const normalized = normalizeFormValues(values)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof FlatSmartRouterDefaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
    form.reset(buildFormDefaults(normalized))
  }

  return (
    <SettingsSection title={t('Smart Router')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'These settings are stored in the database and remain after restart.'
            )}
          </p>
          <FormField
            control={form.control}
            name='smart_router_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable smart router')}</FormLabel>
                  <FormDescription>
                    {t(
                      'When model is auto, pick cheap, mid, or strong models from request complexity.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          {MODEL_FIELDS.map((modelField) => (
            <FormField
              key={modelField.name}
              control={form.control}
              name={`smart_router_setting.${modelField.name}`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t(modelField.labelKey)}</FormLabel>
                  <FormControl>
                    <Combobox
                      options={modelOptions}
                      value={field.value}
                      allowCustomValue
                      onValueChange={(value) => field.onChange(value ?? '')}
                      placeholder={t(modelField.placeholderKey)}
                      searchPlaceholder={t('Search models')}
                      emptyText={t('No models found')}
                      aria-label={t(modelField.labelKey)}
                      className='w-full min-w-0'
                    />
                  </FormControl>
                  {'descriptionKey' in modelField ? (
                    <FormDescription>
                      {t(modelField.descriptionKey)}
                    </FormDescription>
                  ) : null}
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
          <FormField
            control={form.control}
            name='smart_router_setting.classifier_timeout_ms'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Classifier timeout (ms)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={100}
                    max={5000}
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>
                  {t('If classification times out, the mid model is used.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
