package model

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const publicStatusProbeTestBaseSlot = int64(1_700_000_040)

func setupPublicStatusProbeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousType := common.MainDatabaseType()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_busy_timeout=30000", strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)

	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&PublicStatusProbeResult{}, &PublicStatusProbeLease{}))
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
		_ = sqlDB.Close()
	})
	return db
}

func newPublicStatusProbeResult(targetKey string, slotStartedAt, checkedAt int64) *PublicStatusProbeResult {
	pingLatency := int64(125)
	chatLatency := int64(875)
	return &PublicStatusProbeResult{
		TargetKey:     targetKey,
		GroupName:     "codex",
		DisplayName:   "Codex",
		ModelName:     "gpt-5.5",
		ChannelID:     42,
		SlotStartedAt: slotStartedAt,
		CheckedAt:     checkedAt,
		State:         PublicStatusProbeStateOperational,
		PingLatencyMS: &pingLatency,
		ChatLatencyMS: &chatLatency,
	}
}

func TestPublicStatusProbeMigrationValidationAndSanitization(t *testing.T) {
	db := setupPublicStatusProbeTestDB(t)

	assert.True(t, db.Migrator().HasIndex(&PublicStatusProbeResult{}, PublicStatusProbeSlotIndex))
	assert.True(t, db.Migrator().HasIndex(&PublicStatusProbeResult{}, PublicStatusProbeLatestIndex))
	assert.True(t, db.Migrator().HasIndex(&PublicStatusProbeResult{}, PublicStatusProbeCheckedAtIndex))

	result := newPublicStatusProbeResult(" target-a ", publicStatusProbeTestBaseSlot, publicStatusProbeTestBaseSlot+10)
	result.State = PublicStatusProbeState(" OPERATIONAL ")
	result.ErrorCode = " HTTP 5XX !!! "
	inserted, err := CreatePublicStatusProbeResult(result)
	require.NoError(t, err)
	require.True(t, inserted)
	assert.Equal(t, "target-a", result.TargetKey)
	assert.Equal(t, PublicStatusProbeStateOperational, result.State)
	assert.Equal(t, "http_5xx", result.ErrorCode)

	negative := int64(-1)
	for _, mutate := range []func(*PublicStatusProbeResult){
		func(row *PublicStatusProbeResult) { row.PingLatencyMS = &negative },
		func(row *PublicStatusProbeResult) { row.ChatLatencyMS = &negative },
	} {
		row := newPublicStatusProbeResult("target-negative", publicStatusProbeTestBaseSlot+60, publicStatusProbeTestBaseSlot+70)
		mutate(row)
		assert.Error(t, db.Create(row).Error)
	}

	tooLongTarget := newPublicStatusProbeResult(strings.Repeat("a", MaxPublicStatusProbeTargetKeyLength+1), publicStatusProbeTestBaseSlot+120, publicStatusProbeTestBaseSlot+130)
	inserted, err = CreatePublicStatusProbeResult(tooLongTarget)
	assert.Error(t, err)
	assert.False(t, inserted)
	invalidState := newPublicStatusProbeResult("target-a", publicStatusProbeTestBaseSlot, publicStatusProbeTestBaseSlot+20)
	invalidState.State = "sometimes_ok"
	inserted, err = CreatePublicStatusProbeResult(invalidState)
	assert.Error(t, err)
	assert.False(t, inserted)
}

func TestPublicStatusProbeConcurrentResultCreateHasSingleWinner(t *testing.T) {
	setupPublicStatusProbeTestDB(t)
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	type creation struct {
		inserted bool
		err      error
	}
	results := make(chan creation, 2)
	for i := 0; i < 2; i++ {
		go func(worker int) {
			row := newPublicStatusProbeResult("target-race", publicStatusProbeTestBaseSlot, publicStatusProbeTestBaseSlot+int64(worker+1))
			ready <- struct{}{}
			<-start
			inserted, err := CreatePublicStatusProbeResult(row)
			results <- creation{inserted: inserted, err: err}
		}(i)
	}
	<-ready
	<-ready
	close(start)

	insertions := 0
	for i := 0; i < 2; i++ {
		result := <-results
		require.NoError(t, result.err)
		if result.inserted {
			insertions++
		}
	}
	assert.Equal(t, 1, insertions)
	var count int64
	require.NoError(t, DB.Model(&PublicStatusProbeResult{}).Where("target_key = ?", "target-race").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestPublicStatusProbeConditionalCreateRequiresActiveLeaseOwner(t *testing.T) {
	setupPublicStatusProbeTestDB(t)
	const (
		targetKey = "target-conditional"
		ownerID   = "owner-a"
		otherID   = "owner-b"
		now       = publicStatusProbeTestBaseSlot + 10
		leaseEnd  = publicStatusProbeTestBaseSlot + 60
	)
	acquired, err := AcquirePublicStatusProbeLease(targetKey, ownerID, now, leaseEnd)
	require.NoError(t, err)
	require.True(t, acquired)

	wrongOwner := newPublicStatusProbeResult(targetKey, publicStatusProbeTestBaseSlot, now+1)
	inserted, err := CreatePublicStatusProbeResultIfLeaseOwner(context.Background(), wrongOwner, otherID, now+1)
	require.NoError(t, err)
	assert.False(t, inserted)

	expired := newPublicStatusProbeResult(targetKey, publicStatusProbeTestBaseSlot, leaseEnd+1)
	inserted, err = CreatePublicStatusProbeResultIfLeaseOwner(context.Background(), expired, ownerID, leaseEnd)
	require.NoError(t, err)
	assert.False(t, inserted)

	owned := newPublicStatusProbeResult(targetKey, publicStatusProbeTestBaseSlot, now+2)
	inserted, err = CreatePublicStatusProbeResultIfLeaseOwner(context.Background(), owned, ownerID, now+2)
	require.NoError(t, err)
	assert.True(t, inserted)

	duplicate := newPublicStatusProbeResult(targetKey, publicStatusProbeTestBaseSlot, now+3)
	inserted, err = CreatePublicStatusProbeResultIfLeaseOwner(context.Background(), duplicate, ownerID, now+3)
	require.NoError(t, err)
	assert.False(t, inserted)
}

func TestPublicStatusProbeResultExistsByTargetAndSlot(t *testing.T) {
	setupPublicStatusProbeTestDB(t)
	const (
		targetKey = "target-exists"
		slot      = publicStatusProbeTestBaseSlot
	)

	exists, err := PublicStatusProbeResultExists(targetKey, slot)
	require.NoError(t, err)
	assert.False(t, exists)

	inserted, err := CreatePublicStatusProbeResult(newPublicStatusProbeResult(targetKey, slot, slot+10))
	require.NoError(t, err)
	require.True(t, inserted)

	exists, err = PublicStatusProbeResultExists(" target-exists ", slot)
	require.NoError(t, err)
	assert.True(t, exists)
	exists, err = PublicStatusProbeResultExists(targetKey, slot+60)
	require.NoError(t, err)
	assert.False(t, exists)
	exists, err = PublicStatusProbeResultExists("other-target", slot)
	require.NoError(t, err)
	assert.False(t, exists)

	for _, testCase := range []struct {
		name          string
		targetKey     string
		slotStartedAt int64
	}{
		{name: "empty target", targetKey: "", slotStartedAt: slot},
		{name: "non-positive slot", targetKey: targetKey, slotStartedAt: 0},
		{name: "non-boundary slot", targetKey: targetKey, slotStartedAt: slot + 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			exists, err := PublicStatusProbeResultExists(testCase.targetKey, testCase.slotStartedAt)
			assert.Error(t, err)
			assert.False(t, exists)
		})
	}
}

func TestPublicStatusProbeLatestResultsAreCappedAndChronological(t *testing.T) {
	setupPublicStatusProbeTestDB(t)
	for i := 0; i < 65; i++ {
		slot := publicStatusProbeTestBaseSlot + int64(i)*60
		inserted, err := CreatePublicStatusProbeResult(newPublicStatusProbeResult("target-a", slot, slot+10))
		require.NoError(t, err)
		require.True(t, inserted)
	}
	inserted, err := CreatePublicStatusProbeResult(newPublicStatusProbeResult("target-b", publicStatusProbeTestBaseSlot, publicStatusProbeTestBaseSlot+1_000))
	require.NoError(t, err)
	require.True(t, inserted)

	for _, limit := range []int{0, -1, MaxPublicStatusProbeHistory + 1} {
		rows, err := GetLatestPublicStatusProbeResults("target-a", limit)
		require.NoError(t, err)
		require.Len(t, rows, MaxPublicStatusProbeHistory)
		assert.Equal(t, publicStatusProbeTestBaseSlot+5*60+10, rows[0].CheckedAt)
		assert.Equal(t, publicStatusProbeTestBaseSlot+64*60+10, rows[len(rows)-1].CheckedAt)
		for i := 1; i < len(rows); i++ {
			assert.Less(t, rows[i-1].CheckedAt, rows[i].CheckedAt)
		}
	}

	rows, err := GetLatestPublicStatusProbeResults("target-a", 3)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, publicStatusProbeTestBaseSlot+62*60+10, rows[0].CheckedAt)
	assert.Equal(t, publicStatusProbeTestBaseSlot+64*60+10, rows[2].CheckedAt)
}

func TestPublicStatusProbeRetentionDeletesStrictlyBeforeCutoff(t *testing.T) {
	setupPublicStatusProbeTestDB(t)
	cutoff := publicStatusProbeTestBaseSlot + 1_000
	checkedTimes := []int64{cutoff - 1, cutoff, cutoff + 1}
	for i, checkedAt := range checkedTimes {
		slot := publicStatusProbeTestBaseSlot + int64(i)*60
		inserted, err := CreatePublicStatusProbeResult(newPublicStatusProbeResult("target-retention", slot, checkedAt))
		require.NoError(t, err)
		require.True(t, inserted)
	}

	deleted, err := DeletePublicStatusProbeResultsBefore(cutoff)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	var rows []PublicStatusProbeResult
	require.NoError(t, DB.Order("checked_at ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, cutoff, rows[0].CheckedAt)
	assert.Equal(t, cutoff+1, rows[1].CheckedAt)
}

func TestPublicStatusProbeLeaseCASAndOwnerChecks(t *testing.T) {
	setupPublicStatusProbeTestDB(t)
	const (
		targetKey  = "target-lease"
		now        = int64(1_700_000_000)
		leaseUntil = int64(1_700_000_100)
	)
	owners := []string{"owner-a", "owner-b"}
	ready := make(chan struct{}, len(owners))
	start := make(chan struct{})
	type acquisition struct {
		owner    string
		acquired bool
		err      error
	}
	results := make(chan acquisition, len(owners))
	for _, owner := range owners {
		go func(owner string) {
			ready <- struct{}{}
			<-start
			acquired, err := AcquirePublicStatusProbeLease(targetKey, owner, now, leaseUntil)
			results <- acquisition{owner: owner, acquired: acquired, err: err}
		}(owner)
	}
	for range owners {
		<-ready
	}
	close(start)

	winner := ""
	for range owners {
		result := <-results
		require.NoError(t, result.err)
		if result.acquired {
			require.Empty(t, winner)
			winner = result.owner
		}
	}
	require.NotEmpty(t, winner)
	loser := owners[0]
	if loser == winner {
		loser = owners[1]
	}

	renewed, err := RenewPublicStatusProbeLease(targetKey, loser, now+1, leaseUntil+50)
	require.NoError(t, err)
	assert.False(t, renewed)
	released, err := ReleasePublicStatusProbeLease(targetKey, loser)
	require.NoError(t, err)
	assert.False(t, released)

	acquired, err := AcquirePublicStatusProbeLease(targetKey, loser, leaseUntil, leaseUntil+100)
	require.NoError(t, err)
	assert.True(t, acquired)
	renewed, err = RenewPublicStatusProbeLease(targetKey, winner, leaseUntil+1, leaseUntil+150)
	require.NoError(t, err)
	assert.False(t, renewed)
	renewed, err = RenewPublicStatusProbeLease(targetKey, loser, leaseUntil+1, leaseUntil+150)
	require.NoError(t, err)
	assert.True(t, renewed)

	released, err = ReleasePublicStatusProbeLease(targetKey, winner)
	require.NoError(t, err)
	assert.False(t, released)
	released, err = ReleasePublicStatusProbeLease(targetKey, loser)
	require.NoError(t, err)
	assert.True(t, released)

	var lease PublicStatusProbeLease
	require.NoError(t, DB.First(&lease, "target_key = ?", targetKey).Error)
	assert.Empty(t, lease.OwnerID)
	assert.Zero(t, lease.LeaseUntil)
}

func TestPublicStatusProbeLeaseNoOpUpdateConfirmsCurrentOwner(t *testing.T) {
	db := setupPublicStatusProbeTestDB(t)
	const (
		ownerID           = "owner-a"
		otherOwnerID      = "owner-b"
		now               = int64(1_700_000_000)
		acquireLeaseUntil = int64(1_700_000_100)
		renewNow          = int64(1_700_000_010)
		renewLeaseUntil   = int64(1_700_000_200)
	)

	acquired, err := AcquirePublicStatusProbeLease("target-acquire-noop", ownerID, now, acquireLeaseUntil)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = AcquirePublicStatusProbeLease("target-renew-noop", ownerID, now, acquireLeaseUntil)
	require.NoError(t, err)
	require.True(t, acquired)
	renewed, err := RenewPublicStatusProbeLease("target-renew-noop", ownerID, renewNow, renewLeaseUntil)
	require.NoError(t, err)
	require.True(t, renewed)

	const (
		updateCallbackName = "test:public_status_probe_mysql_changed_rows"
		queryCallbackName  = "test:public_status_probe_lease_confirmation"
	)
	fallbackReads := 0
	require.NoError(t, db.Callback().Update().After("gorm:update").Register(updateCallbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "public_status_probe_leases" {
			tx.Statement.RowsAffected = 0
		}
	}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(queryCallbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "public_status_probe_leases" {
			fallbackReads++
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove(updateCallbackName)
		_ = db.Callback().Query().Remove(queryCallbackName)
	})

	acquired, err = AcquirePublicStatusProbeLease("target-acquire-noop", ownerID, now, acquireLeaseUntil)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, 1, fallbackReads)

	renewed, err = RenewPublicStatusProbeLease("target-renew-noop", ownerID, renewNow, renewLeaseUntil)
	require.NoError(t, err)
	assert.True(t, renewed)
	assert.Equal(t, 2, fallbackReads)

	acquired, err = AcquirePublicStatusProbeLease("target-acquire-noop", otherOwnerID, now, acquireLeaseUntil)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Equal(t, 3, fallbackReads)

	renewed, err = RenewPublicStatusProbeLease("target-renew-noop", ownerID, renewLeaseUntil, renewLeaseUntil+100)
	require.NoError(t, err)
	assert.False(t, renewed)
	assert.Equal(t, 4, fallbackReads)
}

func TestCompletePublicStatusProbeLeaseOwnerAndBoundaries(t *testing.T) {
	setupPublicStatusProbeTestDB(t)
	const (
		targetKey        = "target-complete"
		ownerID          = "owner-a"
		otherOwnerID     = "owner-b"
		now              = publicStatusProbeTestBaseSlot + 30
		activeLeaseUntil = publicStatusProbeTestBaseSlot + 120
		holdUntil        = publicStatusProbeTestBaseSlot + 60
	)

	acquired, err := AcquirePublicStatusProbeLease(targetKey, ownerID, now, activeLeaseUntil)
	require.NoError(t, err)
	require.True(t, acquired)

	completed, err := CompletePublicStatusProbeLease(targetKey, otherOwnerID, now+1, holdUntil)
	require.NoError(t, err)
	assert.False(t, completed)
	var lease PublicStatusProbeLease
	require.NoError(t, DB.First(&lease, "target_key = ?", targetKey).Error)
	assert.Equal(t, ownerID, lease.OwnerID)
	assert.Equal(t, activeLeaseUntil, lease.LeaseUntil)
	assert.Equal(t, now, lease.UpdatedAt)

	completed, err = CompletePublicStatusProbeLease(targetKey, ownerID, holdUntil+1, holdUntil)
	assert.Error(t, err)
	assert.False(t, completed)
	completed, err = CompletePublicStatusProbeLease(targetKey, ownerID, now+1, holdUntil+1)
	assert.Error(t, err)
	assert.False(t, completed)

	completedAt := now + 2
	completed, err = CompletePublicStatusProbeLease(" target-complete ", " owner-a ", completedAt, holdUntil)
	require.NoError(t, err)
	assert.True(t, completed)
	require.NoError(t, DB.First(&lease, "target_key = ?", targetKey).Error)
	assert.Empty(t, lease.OwnerID)
	assert.Equal(t, holdUntil, lease.LeaseUntil)
	assert.Equal(t, completedAt, lease.UpdatedAt)

	acquired, err = AcquirePublicStatusProbeLease(targetKey, otherOwnerID, holdUntil-1, holdUntil+60)
	require.NoError(t, err)
	assert.False(t, acquired)
	acquired, err = AcquirePublicStatusProbeLease(targetKey, otherOwnerID, holdUntil, holdUntil+60)
	require.NoError(t, err)
	assert.True(t, acquired)
}

func TestCompletePublicStatusProbeLeaseAllowsHoldAtNow(t *testing.T) {
	setupPublicStatusProbeTestDB(t)
	const (
		targetKey = "target-complete-at-now"
		ownerID   = "owner-a"
		now       = publicStatusProbeTestBaseSlot + 60
	)

	acquired, err := AcquirePublicStatusProbeLease(targetKey, ownerID, now-30, now+60)
	require.NoError(t, err)
	require.True(t, acquired)
	completed, err := CompletePublicStatusProbeLease(targetKey, ownerID, now, now)
	require.NoError(t, err)
	assert.True(t, completed)

	var lease PublicStatusProbeLease
	require.NoError(t, DB.First(&lease, "target_key = ?", targetKey).Error)
	assert.Empty(t, lease.OwnerID)
	assert.Equal(t, now, lease.LeaseUntil)
	assert.Equal(t, now, lease.UpdatedAt)
}

func TestCompletePublicStatusProbeLeaseRejectsExpiredOwner(t *testing.T) {
	setupPublicStatusProbeTestDB(t)
	const (
		targetKey = "target-complete-expired"
		ownerID   = "owner-a"
		now       = publicStatusProbeTestBaseSlot + 30
		leaseEnd  = publicStatusProbeTestBaseSlot + 60
		holdUntil = publicStatusProbeTestBaseSlot + 120
	)

	acquired, err := AcquirePublicStatusProbeLease(targetKey, ownerID, now, leaseEnd)
	require.NoError(t, err)
	require.True(t, acquired)
	completed, err := CompletePublicStatusProbeLease(targetKey, ownerID, leaseEnd, holdUntil)
	require.NoError(t, err)
	assert.False(t, completed)

	var lease PublicStatusProbeLease
	require.NoError(t, DB.First(&lease, "target_key = ?", targetKey).Error)
	assert.Equal(t, ownerID, lease.OwnerID)
	assert.Equal(t, leaseEnd, lease.LeaseUntil)
	assert.Equal(t, now, lease.UpdatedAt)
}

func TestCompletePublicStatusProbeLeaseConcurrentSingleWinner(t *testing.T) {
	setupPublicStatusProbeTestDB(t)
	const (
		targetKey = "target-complete-race"
		ownerID   = "owner-a"
		now       = publicStatusProbeTestBaseSlot + 30
		holdUntil = publicStatusProbeTestBaseSlot + 60
		workers   = 8
	)

	acquired, err := AcquirePublicStatusProbeLease(targetKey, ownerID, now, holdUntil+60)
	require.NoError(t, err)
	require.True(t, acquired)

	ready := make(chan struct{}, workers)
	start := make(chan struct{})
	type completion struct {
		completed bool
		err       error
	}
	results := make(chan completion, workers)
	for i := 0; i < workers; i++ {
		go func() {
			ready <- struct{}{}
			<-start
			completed, err := CompletePublicStatusProbeLease(targetKey, ownerID, now+1, holdUntil)
			results <- completion{completed: completed, err: err}
		}()
	}
	for i := 0; i < workers; i++ {
		<-ready
	}
	close(start)

	winners := 0
	for i := 0; i < workers; i++ {
		result := <-results
		require.NoError(t, result.err)
		if result.completed {
			winners++
		}
	}
	assert.Equal(t, 1, winners)

	var lease PublicStatusProbeLease
	require.NoError(t, DB.First(&lease, "target_key = ?", targetKey).Error)
	assert.Empty(t, lease.OwnerID)
	assert.Equal(t, holdUntil, lease.LeaseUntil)
	assert.Equal(t, now+1, lease.UpdatedAt)
}
