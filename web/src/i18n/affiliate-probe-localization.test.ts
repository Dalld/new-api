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
import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'

import { STATIC_I18N_KEYS } from './static-keys'

type Locale = { translation: Record<string, string> }

const i18nDir = dirname(fileURLToPath(import.meta.url))
const srcDir = join(i18nDir, '..')
const sourceRoots = [
  join(srcDir, 'features', 'affiliate'),
  join(srcDir, 'features', 'my-affiliate'),
  join(srcDir, 'features', 'group-probe-status'),
  join(srcDir, 'features', 'system-settings', 'operations', 'group-probe'),
  join(
    srcDir,
    'features',
    'system-settings',
    'operations',
    'public-status-probe'
  ),
]
const sourceFiles = [
  join(
    srcDir,
    'features',
    'system-settings',
    'billing',
    'affiliate-commission-settings-section.tsx'
  ),
  join(srcDir, 'hooks', 'use-top-nav-links.ts'),
  join(srcDir, 'routes', 'status.tsx'),
]

const operationsSectionRegistry = join(
  srcDir,
  'features',
  'system-settings',
  'operations',
  'section-registry.tsx'
)

const publicStatusProbeDynamicKeys = [
  'Ping timeout (seconds)',
  'Conversation timeout (seconds)',
  'Degraded latency threshold (ms)',
  'Probe concurrency',
  'History retention (days)',
  'OpenAI Chat Completions',
  'OpenAI Responses',
  'Anthropic Messages',
  'Gemini Generate Content',
] as const

function collectSourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      return entry.name === '__tests__' ? [] : collectSourceFiles(path)
    }
    return /\.(?:ts|tsx)$/.test(entry.name) ? [path] : []
  })
}

function collectTranslationKeys() {
  const keys = new Set<string>()
  const files = [...sourceRoots.flatMap(collectSourceFiles), ...sourceFiles]
  for (const file of files) {
    const source = readFileSync(file, 'utf8')
    for (const match of source.matchAll(/\bt\(\s*(['"])(.*?)\1/g)) {
      keys.add(match[2])
    }
  }
  for (const key of publicStatusProbeDynamicKeys) keys.add(key)
  return [...keys].sort()
}

function readLocale(filename: string): Locale {
  return JSON.parse(readFileSync(join(i18nDir, 'locales', filename), 'utf8'))
}

describe('affiliate and channel status localization', () => {
  test('registers the public status probe operations section exactly once', () => {
    const source = readFileSync(operationsSectionRegistry, 'utf8')
    const registrations = source.match(
      /\bid:\s*['"]public-status-probe['"]/g
    )

    assert.equal(registrations?.length, 1)
    assert.match(
      source,
      /import\s*{\s*PublicStatusProbeSettingsSection\s*}\s*from\s*['"]\.\/public-status-probe['"]/
    )
    assert.match(
      source,
      /id:\s*['"]public-status-probe['"][\s\S]*?titleKey:\s*['"]Public Status Probe['"][\s\S]*?build:\s*\(\)\s*=>\s*<PublicStatusProbeSettingsSection\s*\/>/
    )
  })

  test('declares every dynamic public status probe label for extraction', () => {
    for (const key of publicStatusProbeDynamicKeys) {
      assert.ok(STATIC_I18N_KEYS.includes(key), `missing static key: ${key}`)
    }
  })

  test('provides every UI key in all supported locales', () => {
    const en = readLocale('en.json').translation
    const locales = new Map(
      ['fr', 'ja', 'ru', 'vi', 'zh', 'zh-TW'].map((locale) => [
        locale,
        readLocale(`${locale}.json`).translation,
      ])
    )
    const keys = collectTranslationKeys()

    assert.ok(
      keys.length >= 80,
      `expected at least 80 keys, got ${keys.length}`
    )
    for (const key of keys) {
      assert.equal(en[key], key, `missing English base key: ${key}`)
      for (const [locale, translations] of locales) {
        assert.ok(translations[key], `missing ${locale} key: ${key}`)
      }
    }

    const representativeKeys = [
      'Channel Status',
      'Affiliate Management',
      'My Referrals',
      'No invitation records',
      'No commission records',
      'Group Probe',
      'Public group probe status',
      'Failed to load referral data',
    ]
    for (const [locale, translations] of locales) {
      for (const key of representativeKeys) {
        assert.notEqual(
          translations[key],
          key,
          `untranslated ${locale} key: ${key}`
        )
      }
    }

    for (const locale of ['zh', 'zh-TW']) {
      const translations = locales.get(locale)
      assert.ok(translations)
      for (const key of keys) {
        assert.notEqual(
          translations[key],
          key,
          `untranslated ${locale} key: ${key}`
        )
      }
    }
  })

  test('uses the required public navigation label', () => {
    assert.equal(
      readLocale('zh.json').translation['Channel Status'],
      '渠道状态'
    )
    assert.equal(
      readLocale('zh-TW.json').translation['Channel Status'],
      '渠道狀態'
    )
  })
})
