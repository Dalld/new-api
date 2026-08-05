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
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'

const walletSource = readFileSync(
  new URL('./index.tsx', import.meta.url),
  'utf8'
)
const walletApiSource = readFileSync(
  new URL('./api.ts', import.meta.url),
  'utf8'
)
const walletHooksSource = readFileSync(
  new URL('./hooks/index.ts', import.meta.url),
  'utf8'
)

describe('wallet affiliate migration', () => {
  test('removes the referral card and transfer state from Wallet', () => {
    assert.doesNotMatch(walletSource, /AffiliateRewardsCard/)
    assert.doesNotMatch(walletSource, /TransferDialog/)
    assert.doesNotMatch(walletSource, /transferDialogOpen/)
    assert.doesNotMatch(walletSource, /useAffiliate/)
  })

  test('removes affiliate API ownership from Wallet', () => {
    assert.doesNotMatch(walletApiSource, /\/api\/user\/aff(?:_transfer)?/)
    assert.doesNotMatch(walletHooksSource, /use-affiliate/)
  })
})
