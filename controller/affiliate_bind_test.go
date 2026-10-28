package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBindAffiliateInviterControllerSuccessAndAudit(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))

	invitee := model.User{Username: "controller-invitee", Password: "password123", AffCode: "controller-invitee-code", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	inviter := model.User{Username: "controller-inviter", Password: "password123", AffCode: "controller-inviter-code", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&invitee).Error)
	require.NoError(t, db.Create(&inviter).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/affiliate/bind", strings.NewReader(
		`{"invitee_id":`+strconv.Itoa(invitee.Id)+`,"inviter_id":`+strconv.Itoa(inviter.Id)+`}`,
	))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 9999)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root-operator")

	BindAffiliateInviter(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Contains(t, recorder.Body.String(), `"invitee_id":`)

	var auditEntry model.AuditLog
	require.NoError(t, db.Where("action = ?", "affiliate.inviter_bind").First(&auditEntry).Error)
	assert.Equal(t, 9999, auditEntry.UserId)
	require.NotNil(t, auditEntry.Other.Op)
	assert.Equal(t, "affiliate.inviter_bind", auditEntry.Other.Op.Action)
	assert.NotZero(t, auditEntry.Other.Op.Params["invitee_id"])
	assert.NotZero(t, auditEntry.Other.Op.Params["inviter_id"])
	assert.Equal(t, json.RawMessage("0"), auditEntry.Other.Op.Params["previous_inviter_id"])
	require.NotNil(t, auditEntry.Other.AdminInfo)
	assert.Equal(t, "root-operator", auditEntry.Other.AdminInfo.AdminUsername)
	assert.Equal(t, common.RoleRootUser, auditEntry.Other.AdminInfo.AdminRole)
	assert.Equal(t, "session", auditEntry.Other.AdminInfo.AuthMethod)
}

func TestBindAffiliateInviterControllerRejectsInvalidPayload(t *testing.T) {
	setupManageUserTestDB(t)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/affiliate/bind", strings.NewReader(`{"invitee_id":0,"inviter_id":1}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 9999)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root-operator")

	BindAffiliateInviter(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), model.ErrAffiliateBindInvalidInput.Error())
}
