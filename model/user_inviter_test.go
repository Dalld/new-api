package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"
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

func createAffiliateBindUser(t *testing.T, username string, inviterID int) User {
	t.Helper()
	user := User{
		Username:  username,
		Password:  "password123",
		AffCode:   "bind-" + username,
		Role:      common.RoleCommonUser,
		Status:    common.UserStatusEnabled,
		InviterId: inviterID,
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func TestBindAffiliateInviterBindsOnceWithoutRewards(t *testing.T) {
	setupInviterTestDB(t)
	invitee := createAffiliateBindUser(t, "bind-invitee", 0)
	inviter := createAffiliateBindUser(t, "bind-inviter", 0)

	result, err := BindAffiliateInviter(invitee.Id, inviter.Id)
	require.NoError(t, err)
	require.Equal(t, invitee.Id, result.InviteeID)
	require.Equal(t, inviter.Id, result.InviterID)
	require.Zero(t, result.PreviousInviterID)

	var storedInvitee User
	require.NoError(t, DB.First(&storedInvitee, invitee.Id).Error)
	assert.Equal(t, inviter.Id, storedInvitee.InviterId)
	assert.Zero(t, storedInvitee.Quota)
	assert.Zero(t, storedInvitee.AffQuota)
	assert.Zero(t, storedInvitee.AffHistoryQuota)

	var storedInviter User
	require.NoError(t, DB.First(&storedInviter, inviter.Id).Error)
	assert.Equal(t, 1, storedInviter.AffCount)
	assert.Zero(t, storedInviter.AffQuota)
	assert.Zero(t, storedInviter.AffHistoryQuota)

	_, err = BindAffiliateInviter(invitee.Id, inviter.Id)
	require.ErrorIs(t, err, ErrAffiliateBindAlreadyBound)
}

func TestBindAffiliateInviterRejectsInvalidRelationships(t *testing.T) {
	setupInviterTestDB(t)
	invitee := createAffiliateBindUser(t, "invalid-invitee", 0)
	inviter := createAffiliateBindUser(t, "invalid-inviter", 0)
	ancestor := createAffiliateBindUser(t, "invalid-ancestor", 0)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", inviter.Id).Update("inviter_id", ancestor.Id).Error)

	_, err := BindAffiliateInviter(0, inviter.Id)
	require.ErrorIs(t, err, ErrAffiliateBindInvalidInput)

	deletedInvitee := createAffiliateBindUser(t, "deleted-invitee", 0)
	require.NoError(t, DB.Delete(&deletedInvitee).Error)
	_, err = BindAffiliateInviter(deletedInvitee.Id, inviter.Id)
	require.ErrorIs(t, err, ErrAffiliateBindTargetMissing)

	deletedInviter := createAffiliateBindUser(t, "deleted-inviter", 0)
	require.NoError(t, DB.Delete(&deletedInviter).Error)
	_, err = BindAffiliateInviter(invitee.Id, deletedInviter.Id)
	require.ErrorIs(t, err, ErrAffiliateBindInviterMissing)

	_, err = BindAffiliateInviter(invitee.Id, invitee.Id)
	require.ErrorIs(t, err, ErrAffiliateBindSelfReference)

	_, err = BindAffiliateInviter(99999, inviter.Id)
	require.ErrorIs(t, err, ErrAffiliateBindTargetMissing)

	_, err = BindAffiliateInviter(invitee.Id, 99999)
	require.ErrorIs(t, err, ErrAffiliateBindInviterMissing)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", invitee.Id).Update("inviter_id", inviter.Id).Error)
	_, err = BindAffiliateInviter(invitee.Id, ancestor.Id)
	require.ErrorIs(t, err, ErrAffiliateBindAlreadyBound)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", invitee.Id).Update("inviter_id", 0).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", ancestor.Id).Update("inviter_id", invitee.Id).Error)
	_, err = BindAffiliateInviter(invitee.Id, inviter.Id)
	require.ErrorIs(t, err, ErrAffiliateBindCycle)
}

func TestBindAffiliateInviterConcurrentOnlySucceedsOnce(t *testing.T) {
	setupInviterTestDB(t)
	invitee := createAffiliateBindUser(t, "concurrent-invitee", 0)
	inviterA := createAffiliateBindUser(t, "concurrent-inviter-a", 0)
	inviterB := createAffiliateBindUser(t, "concurrent-inviter-b", 0)

	var wg sync.WaitGroup
	results := make(chan error, 2)
	runBind := func(inviterID int) {
		defer wg.Done()
		_, err := BindAffiliateInviter(invitee.Id, inviterID)
		results <- err
	}

	wg.Add(2)
	go runBind(inviterA.Id)
	go runBind(inviterB.Id)
	wg.Wait()
	close(results)

	successCount := 0
	for err := range results {
		if err == nil {
			successCount++
			continue
		}
		assert.True(t,
			errors.Is(err, ErrAffiliateBindAlreadyBound) ||
				errors.Is(err, ErrAffiliateBindConflict),
			"unexpected error: %v", err,
		)
	}
	assert.Equal(t, 1, successCount)

	var storedInvitee User
	require.NoError(t, DB.First(&storedInvitee, invitee.Id).Error)
	assert.Contains(t, []int{inviterA.Id, inviterB.Id}, storedInvitee.InviterId)

	var storedA User
	require.NoError(t, DB.First(&storedA, inviterA.Id).Error)
	var storedB User
	require.NoError(t, DB.First(&storedB, inviterB.Id).Error)
	assert.Equal(t, 1, storedA.AffCount+storedB.AffCount)
}

func TestBindAffiliateInviterHidesUnexpectedDatabaseErrors(t *testing.T) {
	setupInviterTestDB(t)
	invitee := createAffiliateBindUser(t, "closed-db-invitee", 0)
	inviter := createAffiliateBindUser(t, "closed-db-inviter", 0)

	sqlDB, err := DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	_, err = BindAffiliateInviter(invitee.Id, inviter.Id)
	require.ErrorIs(t, err, ErrDatabase)
	assert.NotContains(t, err.Error(), "database is closed")
}
