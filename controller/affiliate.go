package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

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
