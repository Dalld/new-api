package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestStripeCheckoutSessionIsCreatedAfterPendingTopUp(t *testing.T) {
	previousDB := model.DB
	previousFactory := createStripeCheckoutSession
	previousDatabaseType := common.MainDatabaseType()
	previousQuotaPerUnit := common.QuotaPerUnit
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.QuotaPerUnit = 500
	t.Cleanup(func() {
		createStripeCheckoutSession = previousFactory
		model.DB = previousDB
		common.SetMainDatabaseType(previousDatabaseType)
		common.QuotaPerUnit = previousQuotaPerUnit
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	user := model.User{Username: "stripe-ordering", Group: "default", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	var tradeNo string
	createStripeCheckoutSession = func(referenceID, customerID, email string, amount int64, successURL, cancelURL string) (string, error) {
		tradeNo = referenceID
		var topUp model.TopUp
		require.NoError(t, db.Where("trade_no = ?", referenceID).First(&topUp).Error)
		assert.Equal(t, common.TopUpStatusPending, topUp.Status)
		assert.Equal(t, model.PaymentProviderStripe, topUp.PaymentProvider)
		assert.False(t, topUp.CommissionEligible)
		assert.Zero(t, topUp.CommissionInviterID)
		assert.Positive(t, topUp.CommissionBaseQuota)
		assert.NotEmpty(t, topUp.CommissionRate)
		return "", errors.New("stripe unavailable")
	}

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/user/stripe/pay", nil)
	context.Set("id", user.Id)
	stripeAdaptor.RequestPay(context, &StripePayRequest{Amount: 10, PaymentMethod: model.PaymentMethodStripe})

	require.NotEmpty(t, tradeNo)
	var topUp model.TopUp
	require.NoError(t, db.Where("trade_no = ?", tradeNo).First(&topUp).Error)
	assert.Equal(t, common.TopUpStatusFailed, topUp.Status)
	assert.Contains(t, response.Body.String(), "error")
}
