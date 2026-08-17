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
import { z } from 'zod'

import { api } from '@/lib/api'

import type { GlobalSettingsInput, TargetInput } from './types'
import {
  mapPublicStatusProbeError,
  parsePublicStatusProbeResponse,
  targetFormSchema,
  versionedGlobalSettingsSchema,
} from './validation'

const requestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
} as const

const versionSchema = z.number().int().positive()

async function executeRequest<T>(request: () => Promise<T>): Promise<T> {
  try {
    return await request()
  } catch (error) {
    throw mapPublicStatusProbeError(error)
  }
}

export async function getPublicStatusProbeConfig() {
  return executeRequest(async () => {
    const response = await api.get<unknown>(
      '/api/public-status-probe/config',
      requestConfig
    )
    return parsePublicStatusProbeResponse(response.data)
  })
}

export async function updatePublicStatusProbeConfig(
  input: GlobalSettingsInput & { version: number }
) {
  return executeRequest(async () => {
    const payload = versionedGlobalSettingsSchema.parse(input)
    const response = await api.put<unknown>(
      '/api/public-status-probe/config',
      payload,
      requestConfig
    )
    return parsePublicStatusProbeResponse(response.data)
  })
}

export async function createPublicStatusProbeTarget(
  version: number,
  target: TargetInput
) {
  return executeRequest(async () => {
    const payload = {
      version: versionSchema.parse(version),
      ...targetFormSchema.parse(target),
    }
    const response = await api.post<unknown>(
      '/api/public-status-probe/targets',
      payload,
      requestConfig
    )
    return parsePublicStatusProbeResponse(response.data)
  })
}

export async function updatePublicStatusProbeTarget(
  key: string,
  version: number,
  target: TargetInput
) {
  return executeRequest(async () => {
    const payload = {
      version: versionSchema.parse(version),
      ...targetFormSchema.parse(target),
    }
    const response = await api.put<unknown>(
      `/api/public-status-probe/targets/${encodeURIComponent(key)}`,
      payload,
      requestConfig
    )
    return parsePublicStatusProbeResponse(response.data)
  })
}

export async function deletePublicStatusProbeTarget(
  key: string,
  version: number
) {
  return executeRequest(async () => {
    const response = await api.delete<unknown>(
      `/api/public-status-probe/targets/${encodeURIComponent(key)}`,
      {
        params: { version: versionSchema.parse(version) },
        ...requestConfig,
      }
    )
    return parsePublicStatusProbeResponse(response.data)
  })
}
