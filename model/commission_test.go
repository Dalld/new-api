package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func resetCommissionFixtures(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&CommissionRecord{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&CommissionRecord{}).Error)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)
	t.Cleanup(func() {
		DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&CommissionRecord{})
		DB.Exec("DELETE FROM users")
	})
}

func createCommissionUser(t *testing.T, username string, inviterID int) User {
	t.Helper()
	user := User{
		Username:    username,
		Password:    "password123",
		DisplayName: username + " display",
		Email:       username + "@example.com",
		AffCode:     "aff-" + username,
		InviterId:   inviterID,
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func createCommissionRecord(t *testing.T, topUpID int, orderNo string, inviterID, inviteeID int, base, commission int64) CommissionRecord {
	t.Helper()
	record := CommissionRecord{
		TopUpID:             topUpID,
		OrderNo:             orderNo,
		PaymentProvider:     "stripe",
		PaidMoney:           "10.00",
		InviterID:           inviterID,
		InviteeID:           inviteeID,
		CommissionBaseQuota: base,
		CommissionRate:      "0.10",
		CommissionQuota:     commission,
		CreatedAt:           int64(1000 + topUpID),
	}
	inserted, err := InsertCommissionRecord(DB, &record)
	require.NoError(t, err)
	require.True(t, inserted)
	return record
}

func TestCommissionRecordTopUpIDIsUniqueIdempotencyKey(t *testing.T) {
	resetCommissionFixtures(t)
	inviter := createCommissionUser(t, "unique-inviter", 0)
	invitee := createCommissionUser(t, "unique-invitee", inviter.Id)

	first := createCommissionRecord(t, 77, "order-one", inviter.Id, invitee.Id, 1000, 100)
	duplicate := first
	duplicate.ID = 0
	duplicate.OrderNo = "provider-retried-with-another-order-number"

	inserted, err := InsertCommissionRecord(DB, &duplicate)
	require.NoError(t, err)
	assert.False(t, inserted)

	var records []CommissionRecord
	require.NoError(t, DB.Find(&records).Error)
	require.Len(t, records, 1)
	assert.Equal(t, "order-one", records[0].OrderNo)
}

func TestCommissionQueriesEnforceOwnershipAndUseRealRelationships(t *testing.T) {
	resetCommissionFixtures(t)
	inviterA := createCommissionUser(t, "inviter-a", 0)
	inviterB := createCommissionUser(t, "inviter-b", 0)
	commissionOnlyInviter := createCommissionUser(t, "commission-only", 0)
	inviteeA1 := createCommissionUser(t, "invitee-a1", inviterA.Id)
	inviteeA2 := createCommissionUser(t, "invitee-a2", inviterA.Id)
	inviteeB := createCommissionUser(t, "invitee-b", inviterB.Id)

	createCommissionRecord(t, 101, "order-a1", inviterA.Id, inviteeA1.Id, 1000, 100)
	createCommissionRecord(t, 102, "order-b", inviterB.Id, inviteeB.Id, 2000, 200)
	createCommissionRecord(t, 103, "order-historical", commissionOnlyInviter.Id, inviteeA2.Id, 3000, 300)

	invitees, total, err := GetSelfInvitees(inviterA.Id, "", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, invitees, 2)
	for _, invitee := range invitees {
		assert.Equal(t, inviterA.Id, invitee.InviterID)
	}

	selfRecords, total, err := GetSelfCommissionRecords(inviterA.Id, "", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, selfRecords, 1)
	assert.Equal(t, inviterA.Id, selfRecords[0].InviterID)
	assert.Equal(t, int64(100), selfRecords[0].CommissionQuota)

	adminRecords, total, err := GetCommissionRecords("", 1, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, adminRecords, 1)
	assert.NotZero(t, adminRecords[0].InviterID)
	assert.NotZero(t, adminRecords[0].InviteeID)

	relations, total, err := GetAffiliateRelations("", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	byInviter := make(map[int]AffiliateRelation, len(relations))
	for _, relation := range relations {
		byInviter[relation.InviterID] = relation
	}
	assert.Equal(t, int64(2), byInviter[inviterA.Id].InviteeCount)
	assert.Equal(t, int64(100), byInviter[inviterA.Id].CommissionQuota)
	assert.Equal(t, int64(2000), byInviter[inviterB.Id].CommissionBaseQuota)
	assert.Equal(t, int64(300), byInviter[commissionOnlyInviter.Id].CommissionQuota)
}

func TestCommissionSearchTreatsInputAsData(t *testing.T) {
	resetCommissionFixtures(t)
	inviter := createCommissionUser(t, "search-inviter", 0)
	invitee := createCommissionUser(t, "search-invitee", inviter.Id)
	createCommissionRecord(t, 201, "ordinary-order", inviter.Id, invitee.Id, 1000, 100)

	records, total, err := GetCommissionRecords(`' OR 1=1 --`, 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, records)

	for i := 0; i < 3; i++ {
		createCommissionRecord(t, 300+i, fmt.Sprintf("page-%d", i), inviter.Id, invitee.Id, 100, 10)
	}
	records, total, err = GetCommissionRecords("page", 1, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, records, 1)
}
