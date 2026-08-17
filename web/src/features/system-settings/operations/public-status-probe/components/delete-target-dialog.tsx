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
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'

import type { PublicStatusProbeTarget } from '../types'

type DeleteTargetDialogProps = {
  target: PublicStatusProbeTarget | null
  isDeleting: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => Promise<boolean>
}

export function DeleteTargetDialog({
  target,
  isDeleting,
  onOpenChange,
  onConfirm,
}: DeleteTargetDialogProps) {
  const { t } = useTranslation()
  const submittingRef = useRef(false)

  async function handleConfirm() {
    if (submittingRef.current) return
    submittingRef.current = true
    try {
      if (await onConfirm()) onOpenChange(false)
    } finally {
      submittingRef.current = false
    }
  }

  return (
    <ConfirmDialog
      open={target !== null}
      onOpenChange={onOpenChange}
      title={t('Delete probe target')}
      desc={
        target
          ? `${t('Delete "{{name}}" from the public status probes?', {
              name: target.display_name,
            })} (${target.key})`
          : ''
      }
      confirmText={isDeleting ? t('Deleting...') : t('Delete target')}
      destructive
      isLoading={isDeleting}
      handleConfirm={() => void handleConfirm()}
    />
  )
}
