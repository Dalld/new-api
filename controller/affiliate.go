package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/affiliate_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

type selfAffiliateOverview struct {
	InviteeCount               int64   `json:"invitee_count"`
	CommissionRate             float64 `json:"commission_rate"`
	InviterSignupRewardQuota   int     `json:"inviter_signup_reward_quota"`
	InviteeSignupRewardQuota   int     `json:"invitee_signup_reward_quota"`
	PaymentComplianceConfirmed bool    `json:"payment_compliance_confirmed"`
}

// GetAffiliateRelations returns the administrator's inviter aggregates.
func GetAffiliateRelations(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	relations, total, err := model.GetAffiliateRelationsPage(c.Query("keyword"), pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(relations)
	common.ApiSuccess(c, pageInfo)
}

// GetCommissionRecords returns the administrator's global commission audit log.
func GetCommissionRecords(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	records, total, err := model.GetCommissionRecordsPage(c.Query("keyword"), pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(records)
	common.ApiSuccess(c, pageInfo)
}

// GetSelfInvitees returns invitees owned by the authenticated user.
func GetSelfInvitees(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	invitees, total, err := model.GetSelfInviteesPage(
		c.GetInt("id"),
		c.Query("keyword"),
		pageInfo,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(invitees)
	common.ApiSuccess(c, pageInfo)
}

// GetSelfAffiliateOverview returns authoritative counts and public referral
// rules without exposing administrator-only settings.
func GetSelfAffiliateOverview(c *gin.Context) {
	inviteeCount, err := model.GetSelfInviteeCount(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	complianceConfirmed := operation_setting.IsPaymentComplianceConfirmed()
	inviterReward := common.QuotaForInviter
	inviteeReward := common.QuotaForInvitee
	if !complianceConfirmed {
		inviterReward = 0
		inviteeReward = 0
	}

	common.ApiSuccess(c, selfAffiliateOverview{
		InviteeCount:               inviteeCount,
		CommissionRate:             affiliate_setting.GetRate(),
		InviterSignupRewardQuota:   inviterReward,
		InviteeSignupRewardQuota:   inviteeReward,
		PaymentComplianceConfirmed: complianceConfirmed,
	})
}

// GetSelfCommissions returns commission records owned by the authenticated user.
func GetSelfCommissions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	records, total, err := model.GetSelfCommissionRecordsPage(
		c.GetInt("id"),
		c.Query("keyword"),
		pageInfo,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(records)
	common.ApiSuccess(c, pageInfo)
}

// GetSelfRechargeTotal returns the authenticated user's aggregate commission base.
func GetSelfRechargeTotal(c *gin.Context) {
	total, err := model.GetSelfRechargeTotal(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, total)
}
