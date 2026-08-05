package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInviterTestDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	previousType := common.MainDatabaseType()
	previousNewUserQuota := common.QuotaForNewUser
	previousInviteeQuota := common.QuotaForInvitee
	previousInviterQuota := common.QuotaForInviter
	paymentSetting := operation_setting.GetPaymentSetting()
	previousComplianceConfirmed := paymentSetting.ComplianceConfirmed
	previousComplianceVersion := paymentSetting.ComplianceTermsVersion
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}))
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.QuotaForNewUser = 0
	common.QuotaForInvitee = 0
	common.QuotaForInviter = 0
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.QuotaForNewUser = previousNewUserQuota
		common.QuotaForInvitee = previousInviteeQuota
		common.QuotaForInviter = previousInviterQuota
		paymentSetting.ComplianceConfirmed = previousComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = previousComplianceVersion
	})
}

func createInviterFixture(t *testing.T) User {
	t.Helper()
	inviter := User{
		Username: "inviter",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		AffCode:  "INVITE",
	}
	require.NoError(t, DB.Create(&inviter).Error)
	return inviter
}

func TestInsertPersistsInviterRelationshipWhenFixedRewardIsZero(t *testing.T) {
	setupInviterTestDB(t)
	inviter := createInviterFixture(t)
	invitee := User{Username: "invitee", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}

	require.NoError(t, invitee.Insert(inviter.Id))

	var storedInvitee User
	require.NoError(t, DB.First(&storedInvitee, invitee.Id).Error)
	assert.Equal(t, inviter.Id, storedInvitee.InviterId)
	var storedInviter User
	require.NoError(t, DB.First(&storedInviter, inviter.Id).Error)
	assert.Equal(t, 1, storedInviter.AffCount)
	assert.Zero(t, storedInviter.AffQuota)
	assert.Zero(t, storedInviter.AffHistoryQuota)
}

func TestInsertWithTxRollsBackInviterRelationshipWithUser(t *testing.T) {
	setupInviterTestDB(t)
	common.QuotaForInvitee = 25
	common.QuotaForInviter = 50
	inviter := createInviterFixture(t)
	invitee := User{Username: "oauth-invitee", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}

	err := DB.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, invitee.InsertWithTx(tx, inviter.Id))
		return assert.AnError
	})
	require.ErrorIs(t, err, assert.AnError)

	var inviteeCount int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", invitee.Username).Count(&inviteeCount).Error)
	assert.Zero(t, inviteeCount)
	var storedInviter User
	require.NoError(t, DB.First(&storedInviter, inviter.Id).Error)
	assert.Zero(t, storedInviter.AffCount)
	assert.Zero(t, storedInviter.AffQuota)
	assert.Zero(t, storedInviter.AffHistoryQuota)
}

func TestInsertWithTxCommitsRelationshipAndFixedRewardsOnce(t *testing.T) {
	setupInviterTestDB(t)
	common.QuotaForNewUser = 100
	common.QuotaForInvitee = 25
	common.QuotaForInviter = 50
	inviter := createInviterFixture(t)
	invitee := User{Username: "oauth-success", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return invitee.InsertWithTx(tx, inviter.Id)
	}))

	var storedInvitee User
	require.NoError(t, DB.First(&storedInvitee, invitee.Id).Error)
	assert.Equal(t, inviter.Id, storedInvitee.InviterId)
	assert.Equal(t, 125, storedInvitee.Quota)
	var storedInviter User
	require.NoError(t, DB.First(&storedInviter, inviter.Id).Error)
	assert.Equal(t, 1, storedInviter.AffCount)
	assert.Equal(t, 50, storedInviter.AffQuota)
	assert.Equal(t, 50, storedInviter.AffHistoryQuota)
}

func TestInsertRejectsMissingInviterWithoutCreatingUser(t *testing.T) {
	setupInviterTestDB(t)
	invitee := User{Username: "orphan", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}

	err := invitee.Insert(9999)
	require.ErrorContains(t, err, "inviter not found")

	var count int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", invitee.Username).Count(&count).Error)
	assert.Zero(t, count)
}
