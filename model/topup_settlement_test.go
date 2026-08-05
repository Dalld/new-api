package model

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/affiliate_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTopUpSettlementTestDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	previousType := common.MainDatabaseType()
	previousQuotaPerUnit := common.QuotaPerUnit
	previousRate := affiliate_setting.GetRate()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &CommissionRecord{}))
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.QuotaPerUnit = 500
	require.NoError(t, affiliate_setting.SetRate(0.1))
	t.Cleanup(func() {
		_ = sqlDB.Close()
		DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.QuotaPerUnit = previousQuotaPerUnit
		_ = affiliate_setting.SetRate(previousRate)
	})
}

func createSettlementUsers(t *testing.T, withInviter bool) (User, User) {
	t.Helper()
	var inviter User
	if withInviter {
		inviter = User{Username: "settlement-inviter", AffCode: "SETTLE-INVITER"}
		require.NoError(t, DB.Create(&inviter).Error)
	}
	invitee := User{Username: "settlement-invitee", AffCode: "SETTLE-INVITEE", InviterId: inviter.Id, Quota: 100}
	require.NoError(t, DB.Create(&invitee).Error)
	return inviter, invitee
}

func createPendingSettlementTopUp(t *testing.T, invitee User, provider string, amount int64, money float64, base int64) TopUp {
	t.Helper()
	topUp := TopUp{
		UserId:              invitee.Id,
		Amount:              amount,
		Money:               money,
		TradeNo:             "settle-" + provider,
		PaymentMethod:       provider,
		PaymentProvider:     provider,
		CreateTime:          100,
		Status:              common.TopUpStatusPending,
		CommissionBaseQuota: base,
		CommissionEligible:  invitee.InviterId > 0 && base > 0,
		CommissionInviterID: invitee.InviterId,
		CommissionRate:      "0.1",
	}
	require.NoError(t, DB.Create(&topUp).Error)
	return topUp
}

func TestSettleTopUpCreditsQuotaAndCommissionAtomically(t *testing.T) {
	setupTopUpSettlementTestDB(t)
	inviter, invitee := createSettlementUsers(t, true)
	topUp := createPendingSettlementTopUp(t, invitee, PaymentProviderStripe, 3, 2.50, 1000)

	result, err := SettleTopUp(TopUpSettlementRequest{
		TradeNo:                 topUp.TradeNo,
		ExpectedPaymentProvider: PaymentProviderStripe,
		UpdateUser: map[string]interface{}{
			"stripe_customer": "cus_123",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.AlreadySettled)
	assert.EqualValues(t, 1250, result.QuotaAdded)
	assert.EqualValues(t, 100, result.CommissionQuota)

	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	assert.NotZero(t, topUp.CompleteTime)
	require.NoError(t, DB.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 1350, invitee.Quota)
	assert.Equal(t, "cus_123", invitee.StripeCustomer)
	require.NoError(t, DB.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 100, inviter.AffQuota)
	assert.Equal(t, 100, inviter.AffHistoryQuota)

	var record CommissionRecord
	require.NoError(t, DB.Where("top_up_id = ?", topUp.Id).First(&record).Error)
	assert.Equal(t, topUp.TradeNo, record.OrderNo)
	assert.Equal(t, PaymentProviderStripe, record.PaymentProvider)
	assert.Equal(t, "2.50", record.PaidMoney)
	assert.Equal(t, "0.1", record.CommissionRate)
	assert.EqualValues(t, 1000, record.CommissionBaseQuota)
	assert.EqualValues(t, 100, record.CommissionQuota)
}

func TestSettleTopUpUsesExistingProviderQuotaRules(t *testing.T) {
	tests := []struct {
		provider string
		amount   int64
		money    float64
		want     int64
	}{
		{PaymentProviderStripe, 99, 2.5, 1250},
		{PaymentProviderCreem, 730, 2.5, 730},
		{PaymentProviderEpay, 3, 2.5, 1500},
		{PaymentProviderWaffo, 3, 2.5, 1500},
		{PaymentProviderWaffoPancake, 3, 2.5, 1500},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			setupTopUpSettlementTestDB(t)
			_, invitee := createSettlementUsers(t, false)
			topUp := createPendingSettlementTopUp(t, invitee, test.provider, test.amount, test.money, 0)
			result, err := SettleTopUp(TopUpSettlementRequest{TradeNo: topUp.TradeNo, ExpectedPaymentProvider: test.provider})
			require.NoError(t, err)
			assert.Equal(t, test.want, result.QuotaAdded)
			require.NoError(t, DB.First(&invitee, invitee.Id).Error)
			assert.EqualValues(t, 100+test.want, invitee.Quota)
		})
	}
}

func TestSettleTopUpIsIdempotentForRepeatedAndConcurrentCallbacks(t *testing.T) {
	setupTopUpSettlementTestDB(t)
	inviter, invitee := createSettlementUsers(t, true)
	topUp := createPendingSettlementTopUp(t, invitee, PaymentProviderWaffo, 2, 2, 1000)

	const workers = 8
	results := make(chan *TopUpSettlementResult, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := SettleTopUp(TopUpSettlementRequest{TradeNo: topUp.TradeNo, ExpectedPaymentProvider: PaymentProviderWaffo})
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	newSettlements := 0
	for result := range results {
		require.NotNil(t, result)
		if !result.AlreadySettled {
			newSettlements++
		}
	}
	assert.Equal(t, 1, newSettlements)

	require.NoError(t, DB.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 1100, invitee.Quota)
	require.NoError(t, DB.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 100, inviter.AffQuota)
	assert.Equal(t, 100, inviter.AffHistoryQuota)
	var count int64
	require.NoError(t, DB.Model(&CommissionRecord{}).Where("top_up_id = ?", topUp.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestSettleTopUpUsesFrozenDirectInviterAndRateOnly(t *testing.T) {
	setupTopUpSettlementTestDB(t)
	a := User{Username: "settlement-a", AffCode: "SETTLE-A"}
	require.NoError(t, DB.Create(&a).Error)
	b := User{Username: "settlement-b", AffCode: "SETTLE-B", InviterId: a.Id}
	require.NoError(t, DB.Create(&b).Error)
	c := User{Username: "settlement-c", AffCode: "SETTLE-C", InviterId: b.Id, Quota: 100}
	require.NoError(t, DB.Create(&c).Error)
	topUp := createPendingSettlementTopUp(t, c, PaymentProviderStripe, 2, 2, 1000)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", c.Id).Update("inviter_id", a.Id).Error)
	require.NoError(t, affiliate_setting.SetRate(0.5))

	result, err := SettleTopUp(TopUpSettlementRequest{TradeNo: topUp.TradeNo, ExpectedPaymentProvider: PaymentProviderStripe})
	require.NoError(t, err)
	assert.EqualValues(t, 100, result.CommissionQuota)
	require.NoError(t, DB.First(&a, a.Id).Error)
	assert.Zero(t, a.AffQuota)
	assert.Zero(t, a.AffHistoryQuota)
	require.NoError(t, DB.First(&b, b.Id).Error)
	assert.Equal(t, 100, b.AffQuota)
	assert.Equal(t, 100, b.AffHistoryQuota)

	var record CommissionRecord
	require.NoError(t, DB.Where("top_up_id = ?", topUp.Id).First(&record).Error)
	assert.Equal(t, b.Id, record.InviterID)
	assert.Equal(t, c.Id, record.InviteeID)
	assert.Equal(t, "0.1", record.CommissionRate)
}

func TestSettleTopUpDoesNotCommissionHistoricalRows(t *testing.T) {
	setupTopUpSettlementTestDB(t)
	inviter, invitee := createSettlementUsers(t, true)
	topUp := TopUp{
		UserId: invitee.Id, Amount: 2, Money: 2, TradeNo: "historical-topup",
		PaymentMethod: PaymentProviderStripe, PaymentProvider: PaymentProviderStripe,
		CreateTime: 100, Status: common.TopUpStatusPending, CommissionBaseQuota: 1000,
	}
	require.NoError(t, DB.Create(&topUp).Error)

	result, err := SettleTopUp(TopUpSettlementRequest{TradeNo: topUp.TradeNo, ExpectedPaymentProvider: PaymentProviderStripe})
	require.NoError(t, err)
	assert.Zero(t, result.CommissionQuota)
	require.NoError(t, DB.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota)
	assert.Zero(t, inviter.AffHistoryQuota)
	var count int64
	require.NoError(t, DB.Model(&CommissionRecord{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestSettleTopUpRejectsProviderMismatchWithoutWrites(t *testing.T) {
	setupTopUpSettlementTestDB(t)
	_, invitee := createSettlementUsers(t, false)
	topUp := createPendingSettlementTopUp(t, invitee, PaymentProviderStripe, 2, 2, 1000)

	result, err := SettleTopUp(TopUpSettlementRequest{TradeNo: topUp.TradeNo, ExpectedPaymentProvider: PaymentProviderCreem})
	assert.ErrorIs(t, err, ErrPaymentMethodMismatch)
	assert.Nil(t, result)
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	require.NoError(t, DB.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 100, invitee.Quota)
}

func TestSettleTopUpRollsBackEveryWriteWhenCommissionCreditFails(t *testing.T) {
	setupTopUpSettlementTestDB(t)
	inviter, invitee := createSettlementUsers(t, true)
	topUp := createPendingSettlementTopUp(t, invitee, PaymentProviderStripe, 2, 2, 1000)
	require.NoError(t, DB.Exec(`CREATE TRIGGER fail_commission_credit BEFORE UPDATE OF aff_quota ON users
		WHEN NEW.id = `+fmt.Sprint(inviter.Id)+` BEGIN SELECT RAISE(FAIL, 'forced commission credit failure'); END`).Error)

	result, err := SettleTopUp(TopUpSettlementRequest{TradeNo: topUp.TradeNo, ExpectedPaymentProvider: PaymentProviderStripe})
	require.Error(t, err)
	assert.Nil(t, result)
	require.NoError(t, DB.First(&topUp, topUp.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Zero(t, topUp.CompleteTime)
	require.NoError(t, DB.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 100, invitee.Quota)
	require.NoError(t, DB.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota)
	assert.Zero(t, inviter.AffHistoryQuota)
	var count int64
	require.NoError(t, DB.Model(&CommissionRecord{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestSettleTopUpSkipsIneligibleCommissionButStillRecharges(t *testing.T) {
	tests := []struct {
		name        string
		withInviter bool
		base        int64
		rate        float64
	}{
		{"no inviter", false, 1000, 0.1},
		{"zero base", true, 0, 0.1},
		{"rounded zero", true, 1, 0.1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTopUpSettlementTestDB(t)
			require.NoError(t, affiliate_setting.SetRate(test.rate))
			inviter, invitee := createSettlementUsers(t, test.withInviter)
			topUp := createPendingSettlementTopUp(t, invitee, PaymentProviderCreem, 10, 1, test.base)
			result, err := SettleTopUp(TopUpSettlementRequest{TradeNo: topUp.TradeNo, ExpectedPaymentProvider: PaymentProviderCreem})
			require.NoError(t, err)
			assert.EqualValues(t, 10, result.QuotaAdded)
			assert.Zero(t, result.CommissionQuota)
			var count int64
			require.NoError(t, DB.Model(&CommissionRecord{}).Count(&count).Error)
			assert.Zero(t, count)
			if test.withInviter {
				require.NoError(t, DB.First(&inviter, inviter.Id).Error)
				assert.Zero(t, inviter.AffQuota)
				assert.Zero(t, inviter.AffHistoryQuota)
			}
		})
	}
}

func TestSettleTopUpDoesNotRechargeSubscriptionCompatibilityRows(t *testing.T) {
	setupTopUpSettlementTestDB(t)
	_, invitee := createSettlementUsers(t, false)
	topUp := TopUp{
		UserId: invitee.Id, TradeNo: "subscription-compatible", PaymentMethod: PaymentProviderStripe,
		PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess, CommissionBaseQuota: 0,
	}
	require.NoError(t, DB.Create(&topUp).Error)

	result, err := SettleTopUp(TopUpSettlementRequest{TradeNo: topUp.TradeNo, ExpectedPaymentProvider: PaymentProviderStripe})
	require.NoError(t, err)
	assert.True(t, result.AlreadySettled)
	assert.Zero(t, result.QuotaAdded)
	require.NoError(t, DB.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 100, invitee.Quota)
}
