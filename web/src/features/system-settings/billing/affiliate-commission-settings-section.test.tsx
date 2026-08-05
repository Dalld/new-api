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
import { describe, test } from 'node:test'

import {
  buildAffiliateCommissionUpdate,
  commissionPercentToRate,
  commissionRateToPercent,
  createAffiliateCommissionSchema,
} from './affiliate-commission-settings-section'

const affiliateCommissionSchema = createAffiliateCommissionSchema((key) => key)

describe('affiliate commission settings', () => {
  for (const commissionRatePercent of [0, 0.01, 25, 99.99, 100]) {
    test(`accepts a finite percentage in range: ${commissionRatePercent}`, () => {
      assert.equal(
        affiliateCommissionSchema.safeParse({ commissionRatePercent }).success,
        true
      )
    })
  }

  for (const commissionRatePercent of [
    Number.NaN,
    Number.POSITIVE_INFINITY,
    Number.NEGATIVE_INFINITY,
  ]) {
    test(`rejects a non-finite percentage: ${commissionRatePercent}`, () => {
      assert.equal(
        affiliateCommissionSchema.safeParse({ commissionRatePercent }).success,
        false
      )
    })
  }

  for (const commissionRatePercent of [-0.01, 100.01]) {
    test(`rejects an out-of-range percentage: ${commissionRatePercent}`, () => {
      assert.equal(
        affiliateCommissionSchema.safeParse({ commissionRatePercent }).success,
        false
      )
    })
  }

  test('converts displayed percentages to stored decimal rates', () => {
    assert.equal(commissionPercentToRate(0), 0)
    assert.equal(commissionPercentToRate(25), 0.25)
    assert.equal(commissionPercentToRate(100), 1)
  })

  test('builds a valid update request when saving 0%', () => {
    assert.deepEqual(buildAffiliateCommissionUpdate(0), {
      key: 'affiliate_setting.commission_rate',
      value: 0,
    })
  })

  test('converts stored decimal rates to displayed percentages', () => {
    assert.equal(commissionRateToPercent(0), 0)
    assert.equal(commissionRateToPercent(0.125), 12.5)
    assert.equal(commissionRateToPercent(1), 100)
  })
})
