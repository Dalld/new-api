package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/affiliate_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type affiliateAPIResponse[T any] struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type affiliatePage[T any] struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
	Items    []T `json:"items"`
}

type selfAffiliateOverviewResponse struct {
	InviteeCount             int64   `json:"invitee_count"`
	CommissionRate           float64 `json:"commission_rate"`
	InviterSignupRewardQuota int     `json:"inviter_signup_reward_quota"`
	InviteeSignupRewardQuota int     `json:"invitee_signup_reward_quota"`
	PaymentCompliance        bool    `json:"payment_compliance_confirmed"`
}

func setupAffiliateControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CommissionRecord{}))
}

func createAffiliateControllerUser(t *testing.T, username string, inviterID int, createdAt int64) model.User {
	t.Helper()
	user := model.User{
		Username:        username,
		Password:        "password123",
		DisplayName:     username + " display",
		Email:           username + "@example.com",
		AffCode:         "aff-" + username,
		AffQuota:        75,
		AffHistoryQuota: 125,
		InviterId:       inviterID,
		CreatedAt:       createdAt,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	return user
}

func createAffiliateControllerCommission(
	t *testing.T,
	topUpID int,
	orderNo string,
	inviterID int,
	inviteeID int,
	baseQuota int64,
	commissionQuota int64,
	createdAt int64,
) model.CommissionRecord {
	t.Helper()
	record := model.CommissionRecord{
		TopUpID:             topUpID,
		OrderNo:             orderNo,
		PaymentProvider:     "stripe",
		PaidMoney:           "19.95",
		InviterID:           inviterID,
		InviteeID:           inviteeID,
		CommissionBaseQuota: baseQuota,
		CommissionRate:      "0.15",
		CommissionQuota:     commissionQuota,
		CreatedAt:           createdAt,
	}
	inserted, err := model.InsertCommissionRecord(model.DB, &record)
	require.NoError(t, err)
	require.True(t, inserted)
	return record
}

func runAffiliateController(
	t *testing.T,
	handler gin.HandlerFunc,
	target string,
	userID int,
) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", userID)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	handler(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	return recorder
}

func decodeAffiliateResponse[T any](t *testing.T, recorder *httptest.ResponseRecorder) affiliateAPIResponse[T] {
	t.Helper()
	var response affiliateAPIResponse[T]
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Empty(t, response.Message)
	return response
}

func TestAffiliateSelfEndpointsEnforceAuthenticatedOwnership(t *testing.T) {
	setupAffiliateControllerTestDB(t)
	previousPaymentSetting := *operation_setting.GetPaymentSetting()
	previousInviterQuota := common.QuotaForInviter
	previousInviteeQuota := common.QuotaForInvitee
	previousCommissionRate := affiliate_setting.GetRate()
	t.Cleanup(func() {
		*operation_setting.GetPaymentSetting() = previousPaymentSetting
		common.QuotaForInviter = previousInviterQuota
		common.QuotaForInvitee = previousInviteeQuota
		require.NoError(t, affiliate_setting.SetRate(previousCommissionRate))
	})
	operation_setting.GetPaymentSetting().ComplianceConfirmed = false
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = ""
	common.QuotaForInviter = 100
	common.QuotaForInvitee = 50
	inviterA := createAffiliateControllerUser(t, "owner-a", 0, 100)
	inviterB := createAffiliateControllerUser(t, "owner-b", 0, 200)
	inviteeA := createAffiliateControllerUser(t, "invitee-a", inviterA.Id, 300)
	inviteeB := createAffiliateControllerUser(t, "invitee-b", inviterB.Id, 400)
	createAffiliateControllerCommission(t, 101, "order-owner-a", inviterA.Id, inviteeA.Id, 1000, 150, 500)
	createAffiliateControllerCommission(t, 102, "order-owner-b", inviterB.Id, inviteeB.Id, 9000, 1350, 600)

	inviteesResponse := decodeAffiliateResponse[affiliatePage[model.SelfInvitee]](
		t,
		runAffiliateController(
			t,
			GetSelfInvitees,
			"/api/user/aff/invitees?user_id="+strconv.Itoa(inviterB.Id),
			inviterA.Id,
		),
	)
	require.Equal(t, 1, inviteesResponse.Data.Total)
	require.Len(t, inviteesResponse.Data.Items, 1)
	assert.Equal(t, inviteeA.Id, inviteesResponse.Data.Items[0].ID)
	assert.Equal(t, inviterA.Id, inviteesResponse.Data.Items[0].InviterID)

	commissionsResponse := decodeAffiliateResponse[affiliatePage[model.SelfCommissionRecord]](
		t,
		runAffiliateController(
			t,
			GetSelfCommissions,
			"/api/user/aff/commissions?user_id="+strconv.Itoa(inviterB.Id),
			inviterA.Id,
		),
	)
	require.Equal(t, 1, commissionsResponse.Data.Total)
	require.Len(t, commissionsResponse.Data.Items, 1)
	assert.Equal(t, "order-owner-a", commissionsResponse.Data.Items[0].OrderNo)
	assert.Equal(t, inviterA.Id, commissionsResponse.Data.Items[0].InviterID)
	assert.NotEqual(t, inviterB.Id, commissionsResponse.Data.Items[0].InviterID)

	rechargeResponse := decodeAffiliateResponse[int64](
		t,
		runAffiliateController(
			t,
			GetSelfRechargeTotal,
			"/api/user/aff/recharge_total?user_id="+strconv.Itoa(inviterB.Id),
			inviterA.Id,
		),
	)
	assert.Equal(t, int64(1000), rechargeResponse.Data)

	overviewResponse := decodeAffiliateResponse[selfAffiliateOverviewResponse](
		t,
		runAffiliateController(t, GetSelfAffiliateOverview, "/api/user/aff/overview", inviterA.Id),
	)
	assert.Equal(t, int64(1), overviewResponse.Data.InviteeCount)
	assert.False(t, overviewResponse.Data.PaymentCompliance)
	assert.Zero(t, overviewResponse.Data.InviterSignupRewardQuota)
	assert.Zero(t, overviewResponse.Data.InviteeSignupRewardQuota)

	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	require.NoError(t, affiliate_setting.SetRate(0.15))
	overviewResponse = decodeAffiliateResponse[selfAffiliateOverviewResponse](
		t,
		runAffiliateController(t, GetSelfAffiliateOverview, "/api/user/aff/overview", inviterA.Id),
	)
	assert.True(t, overviewResponse.Data.PaymentCompliance)
	assert.Equal(t, 0.15, overviewResponse.Data.CommissionRate)
	assert.Equal(t, 100, overviewResponse.Data.InviterSignupRewardQuota)
	assert.Equal(t, 50, overviewResponse.Data.InviteeSignupRewardQuota)
}

func TestAffiliateSelfListsApplyKeywordAndPageInfo(t *testing.T) {
	setupAffiliateControllerTestDB(t)
	inviter := createAffiliateControllerUser(t, "search-owner", 0, 100)
	newer := createAffiliateControllerUser(t, "needle-newer", inviter.Id, 400)
	older := createAffiliateControllerUser(t, "needle-older", inviter.Id, 300)
	other := createAffiliateControllerUser(t, "unrelated", inviter.Id, 200)
	createAffiliateControllerCommission(t, 201, "needle-order-newer", inviter.Id, newer.Id, 3000, 450, 700)
	createAffiliateControllerCommission(t, 202, "needle-order-older", inviter.Id, older.Id, 2000, 300, 600)
	createAffiliateControllerCommission(t, 203, "other-order", inviter.Id, other.Id, 1000, 150, 500)

	inviteesResponse := decodeAffiliateResponse[affiliatePage[model.SelfInvitee]](
		t,
		runAffiliateController(
			t,
			GetSelfInvitees,
			"/api/user/aff/invitees?keyword=needle&p=2&page_size=1",
			inviter.Id,
		),
	)
	assert.Equal(t, 2, inviteesResponse.Data.Page)
	assert.Equal(t, 1, inviteesResponse.Data.PageSize)
	assert.Equal(t, 2, inviteesResponse.Data.Total)
	require.Len(t, inviteesResponse.Data.Items, 1)
	assert.Equal(t, older.Id, inviteesResponse.Data.Items[0].ID)

	commissionsResponse := decodeAffiliateResponse[affiliatePage[model.SelfCommissionRecord]](
		t,
		runAffiliateController(
			t,
			GetSelfCommissions,
			"/api/user/aff/commissions?keyword=needle&p=2&page_size=1",
			inviter.Id,
		),
	)
	assert.Equal(t, 2, commissionsResponse.Data.Page)
	assert.Equal(t, 1, commissionsResponse.Data.PageSize)
	assert.Equal(t, 2, commissionsResponse.Data.Total)
	require.Len(t, commissionsResponse.Data.Items, 1)
	assert.Equal(t, "needle-order-older", commissionsResponse.Data.Items[0].OrderNo)
}

func TestAffiliateListEndpointsReturnEmptyArrays(t *testing.T) {
	setupAffiliateControllerTestDB(t)

	tests := []struct {
		name    string
		handler gin.HandlerFunc
		target  string
	}{
		{name: "self invitees", handler: GetSelfInvitees, target: "/api/user/aff/invitees"},
		{name: "self commissions", handler: GetSelfCommissions, target: "/api/user/aff/commissions"},
		{name: "admin relations", handler: GetAffiliateRelations, target: "/api/affiliate/relations"},
		{name: "admin commissions", handler: GetCommissionRecords, target: "/api/affiliate/commissions"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := runAffiliateController(t, test.handler, test.target, 1)
			var response affiliateAPIResponse[struct {
				Items json.RawMessage `json:"items"`
			}]
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success, response.Message)
			assert.JSONEq(t, "[]", string(response.Data.Items))
		})
	}
}

func TestAffiliateAdminEndpointsReturnCompleteAuditFields(t *testing.T) {
	setupAffiliateControllerTestDB(t)
	inviter := createAffiliateControllerUser(t, "audit-inviter", 0, 100)
	invitee := createAffiliateControllerUser(t, "audit-invitee", inviter.Id, 200)
	record := createAffiliateControllerCommission(
		t,
		301,
		"audit-order",
		inviter.Id,
		invitee.Id,
		5000,
		750,
		300,
	)

	relationsResponse := decodeAffiliateResponse[affiliatePage[model.AffiliateRelation]](
		t,
		runAffiliateController(
			t,
			GetAffiliateRelations,
			"/api/affiliate/relations?keyword=audit-inviter&p=1&page_size=1",
			0,
		),
	)
	require.Equal(t, 1, relationsResponse.Data.Total)
	require.Len(t, relationsResponse.Data.Items, 1)
	relation := relationsResponse.Data.Items[0]
	assert.Equal(t, inviter.Id, relation.InviterID)
	assert.Equal(t, inviter.Username, relation.Username)
	assert.Equal(t, inviter.DisplayName, relation.DisplayName)
	assert.Equal(t, inviter.Email, relation.Email)
	assert.Equal(t, inviter.AffCode, relation.AffCode)
	assert.Equal(t, inviter.AffQuota, relation.AffQuota)
	assert.Equal(t, inviter.AffHistoryQuota, relation.AffHistoryQuota)
	assert.Equal(t, int64(1), relation.InviteeCount)
	assert.Equal(t, int64(5000), relation.CommissionBaseQuota)
	assert.Equal(t, int64(750), relation.CommissionQuota)
	assert.Equal(t, inviter.CreatedAt, relation.CreatedAt)

	commissionsResponse := decodeAffiliateResponse[affiliatePage[model.CommissionRecordDetail]](
		t,
		runAffiliateController(
			t,
			GetCommissionRecords,
			"/api/affiliate/commissions?keyword=audit-order&p=1&page_size=1",
			0,
		),
	)
	require.Equal(t, 1, commissionsResponse.Data.Total)
	require.Len(t, commissionsResponse.Data.Items, 1)
	commission := commissionsResponse.Data.Items[0]
	assert.Equal(t, record.ID, commission.ID)
	assert.Equal(t, record.TopUpID, commission.TopUpID)
	assert.Equal(t, record.OrderNo, commission.OrderNo)
	assert.Equal(t, record.PaymentProvider, commission.PaymentProvider)
	assert.Equal(t, record.PaidMoney, commission.PaidMoney)
	assert.Equal(t, inviter.Id, commission.InviterID)
	assert.Equal(t, invitee.Id, commission.InviteeID)
	assert.Equal(t, record.CommissionBaseQuota, commission.CommissionBaseQuota)
	assert.Equal(t, record.CommissionRate, commission.CommissionRate)
	assert.Equal(t, record.CommissionQuota, commission.CommissionQuota)
	assert.Equal(t, record.CreatedAt, commission.CreatedAt)
	assert.Equal(t, inviter.Username, commission.InviterUsername)
	assert.Equal(t, invitee.Username, commission.InviteeUsername)
}

func TestAffiliateControllerUsesStandardErrorEnvelope(t *testing.T) {
	setupAffiliateControllerTestDB(t)
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	recorder := runAffiliateController(t, GetSelfInvitees, "/api/user/aff/invitees", 1)
	var response affiliateAPIResponse[affiliatePage[model.SelfInvitee]]
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.NotEmpty(t, response.Message)
}
