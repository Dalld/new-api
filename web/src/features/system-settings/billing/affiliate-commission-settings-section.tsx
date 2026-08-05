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
/* eslint-disable react-refresh/only-export-components */
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

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

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

type Translate = (key: string) => string

export const createAffiliateCommissionSchema = (t: Translate) =>
  z.object({
    commissionRatePercent: z
      .number()
      .finite()
      .min(0, { message: t('Commission rate cannot be less than 0%') })
      .max(100, { message: t('Commission rate cannot exceed 100%') }),
  })

type AffiliateCommissionFormValues = z.infer<
  ReturnType<typeof createAffiliateCommissionSchema>
>

export function commissionRateToPercent(rate: number) {
  return rate * 100
}

export function commissionPercentToRate(percent: number) {
  return percent / 100
}

export function buildAffiliateCommissionUpdate(percent: number) {
  return {
    key: 'affiliate_setting.commission_rate',
    value: commissionPercentToRate(percent),
  }
}

type AffiliateCommissionSettingsSectionProps = {
  defaultValue: number
}

export function AffiliateCommissionSettingsSection({
  defaultValue,
}: AffiliateCommissionSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const defaultPercent = commissionRateToPercent(defaultValue)
  const schema = useMemo(() => createAffiliateCommissionSchema(t), [t])

  const form = useForm<AffiliateCommissionFormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      commissionRatePercent: defaultPercent,
    },
  })

  useEffect(() => {
    form.reset({ commissionRatePercent: defaultPercent })
  }, [defaultPercent, form])

  async function onSubmit(values: AffiliateCommissionFormValues) {
    await updateOption.mutateAsync(
      buildAffiliateCommissionUpdate(values.commissionRatePercent)
    )
    form.reset(values)
  }

  const { isDirty, isSubmitting } = form.formState

  return (
    <SettingsSection title={t('Affiliate Commission')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel={t('Save affiliate commission settings')}
          />

          <FormField
            control={form.control}
            name='commissionRatePercent'
            render={({ field }) => (
              <FormItem className='max-w-sm'>
                <FormLabel>{t('Commission rate (%)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={100}
                    step='any'
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Percentage of an invited user recharge paid as commission'
                  )}
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
