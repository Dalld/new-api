package controller

import (
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
	require.NoError(t, db.AutoMigrate(&model.Log{}))

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

	var logEntry model.Log
	require.NoError(t, db.Where("type = ? AND content LIKE ?", model.LogTypeManage, "%Bound inviter%").First(&logEntry).Error)
	assert.Equal(t, 9999, logEntry.UserId)
	assert.Contains(t, logEntry.Other, `"action":"affiliate.inviter_bind"`)
	assert.Contains(t, logEntry.Other, `"invitee_id":`)
	assert.Contains(t, logEntry.Other, `"inviter_id":`)
	assert.Contains(t, logEntry.Other, `"previous_inviter_id":0`)
	assert.Contains(t, logEntry.Other, `"admin_username":"root-operator"`)
	assert.Contains(t, logEntry.Other, `"admin_role":100`)
	assert.Contains(t, logEntry.Other, `"auth_method":"session"`)
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
