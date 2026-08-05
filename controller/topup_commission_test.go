package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/affiliate_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreezeCommissionBaseQuotaUsesCanonicalPaidMoney(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previousQuotaPerUnit })

	base, err := freezeCommissionBaseQuota(8.004, decimal.NewFromInt(2))

	require.NoError(t, err)
	assert.Equal(t, int64(2_000_000), base)
}

func TestFreezeCommissionBaseQuotaExcludesDiscountedValue(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previousQuotaPerUnit })

	base, err := freezeCommissionBaseQuota(8, decimal.NewFromInt(1))

	require.NoError(t, err)
	assert.Equal(t, int64(4_000_000), base)
}

func TestFreezeCommissionBaseQuotaRejectsInvalidUnitPrice(t *testing.T) {
	_, err := freezeCommissionBaseQuota(10, decimal.Zero)
	assert.Error(t, err)
}

func TestFreezeCommissionSnapshotPersistsDirectRelationshipAndRate(t *testing.T) {
	previousRate := affiliate_setting.GetRate()
	require.NoError(t, affiliate_setting.SetRate(0.2))
	t.Cleanup(func() { _ = affiliate_setting.SetRate(previousRate) })

	user := &model.User{Id: 30, InviterId: 20}
	topUp := &model.TopUp{UserId: user.Id}
	require.NoError(t, freezeCommissionSnapshot(topUp, user, 1000))

	assert.True(t, topUp.CommissionEligible)
	assert.Equal(t, 20, topUp.CommissionInviterID)
	assert.Equal(t, "0.2", topUp.CommissionRate)
	assert.EqualValues(t, 1000, topUp.CommissionBaseQuota)

	user.InviterId = 10
	require.NoError(t, affiliate_setting.SetRate(0.5))
	assert.Equal(t, 20, topUp.CommissionInviterID)
	assert.Equal(t, "0.2", topUp.CommissionRate)
}

func TestFreezeCommissionSnapshotMarksMissingInviterIneligible(t *testing.T) {
	previousRate := affiliate_setting.GetRate()
	require.NoError(t, affiliate_setting.SetRate(0.2))
	t.Cleanup(func() { _ = affiliate_setting.SetRate(previousRate) })

	topUp := &model.TopUp{UserId: 30}
	require.NoError(t, freezeCommissionSnapshot(topUp, &model.User{Id: 30}, 1000))
	assert.False(t, topUp.CommissionEligible)
	assert.Zero(t, topUp.CommissionInviterID)
	assert.Equal(t, "0.2", topUp.CommissionRate)
	assert.EqualValues(t, 1000, topUp.CommissionBaseQuota)
}
