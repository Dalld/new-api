package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type affiliateBindRequest struct {
	InviteeID int `json:"invitee_id" binding:"required,min=1"`
	InviterID int `json:"inviter_id" binding:"required,min=1"`
}

func BindAffiliateInviter(c *gin.Context) {
	var req affiliateBindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, model.ErrAffiliateBindInvalidInput)
		return
	}

	result, err := model.BindAffiliateInviter(req.InviteeID, req.InviterID)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	recordManageAuditFor(c, result.InviteeID, "affiliate.inviter_bind", map[string]interface{}{
		"invitee_id":          result.InviteeID,
		"inviter_id":          result.InviterID,
		"previous_inviter_id": result.PreviousInviterID,
	})
	common.ApiSuccess(c, result)
}
