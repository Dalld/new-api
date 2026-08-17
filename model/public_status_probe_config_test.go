package model

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func usePublicStatusProbeConfigDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "options.db")) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))

	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func preservePublicStatusProbeRuntime(t *testing.T) {
	t.Helper()

	previous := publicstatusprobesetting.CurrentDocument()
	previousLoaded := publicStatusProbeConfigLoaded.Load()
	common.OptionMapRWMutex.Lock()
	previousMap := common.OptionMap
	testMap := make(map[string]string, len(previousMap))
	for key, value := range previousMap {
		testMap[key] = value
	}
	common.OptionMap = testMap
	common.OptionMapRWMutex.Unlock()
	previousHook := publicstatusprobesetting.SetPublishHook(nil)
	t.Cleanup(func() {
		publicstatusprobesetting.SetPublishHook(nil)
		require.NoError(t, publicstatusprobesetting.PublishDocument(previous))
		publicstatusprobesetting.SetPublishHook(previousHook)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousMap
		common.OptionMapRWMutex.Unlock()
		publicStatusProbeConfigLoaded.Store(previousLoaded)
	})
}

func useInitialPublicStatusProbeRuntime(t *testing.T) {
	t.Helper()
	preservePublicStatusProbeRuntime(t)
	publicstatusprobesetting.SetPublishHook(nil)
	publicStatusProbeConfigLoaded.Store(false)
	require.NoError(t, publicstatusprobesetting.PublishDocument(publicstatusprobesetting.DefaultDocument()))
	common.OptionMapRWMutex.Lock()
	delete(common.OptionMap, publicstatusprobesetting.OptionKey)
	common.OptionMapRWMutex.Unlock()
}

func TestGenericOptionWritersRejectPublicStatusProbeConfigBeforeDBWrite(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	saved, _, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(5))
	require.NoError(t, err)
	rawBefore := requirePublicStatusProbeOptionRaw(t, db)
	runtimeBefore := publicstatusprobesetting.CurrentDocument()
	require.NoError(t, db.Create(&Option{Key: "test.bulk-guard", Value: "before"}).Error)

	next := saved
	next.Version++
	next.Enabled = false
	nextRaw, err := publicstatusprobesetting.EncodeDocument(next)
	require.NoError(t, err)

	require.ErrorIs(t, UpdateOption(publicstatusprobesetting.OptionKey, nextRaw), ErrPublicStatusProbeConfigRequiresCAS)
	require.ErrorIs(t, UpdateOptionsBulk(map[string]string{
		publicstatusprobesetting.OptionKey: nextRaw,
		"test.bulk-guard":                  "after",
	}), ErrPublicStatusProbeConfigRequiresCAS)
	assert.Equal(t, rawBefore, requirePublicStatusProbeOptionRaw(t, db))
	assert.Equal(t, runtimeBefore, publicstatusprobesetting.CurrentDocument())
	var guarded Option
	require.NoError(t, db.Where("key = ?", "test.bulk-guard").Take(&guarded).Error)
	assert.Equal(t, "before", guarded.Value)
}

func TestGenericOptionWritersRejectEquivalentPublicStatusProbeKeysBeforeDBWrite(t *testing.T) {
	keys := []string{
		strings.ToLower(publicstatusprobesetting.OptionKey),
		"pUbLiCsTaTuSpRoBeCoNfIg",
		" \t" + publicstatusprobesetting.OptionKey + "\r\n",
		"PúblicStatusProbeConfig",
	}
	for _, key := range keys {
		t.Run(fmt.Sprintf("%q", key), func(t *testing.T) {
			db := usePublicStatusProbeConfigDB(t)
			useInitialPublicStatusProbeRuntime(t)
			_, _, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(6))
			require.NoError(t, err)
			rawBefore := requirePublicStatusProbeOptionRaw(t, db)
			require.NoError(t, db.Create(&Option{Key: "test.equivalent-bulk-guard", Value: "before"}).Error)

			require.ErrorIs(t, UpdateOption(key, "not-json"), ErrPublicStatusProbeConfigRequiresCAS)
			require.ErrorIs(t, UpdateOptionsBulk(map[string]string{
				key:                          "not-json",
				"test.equivalent-bulk-guard": "after",
			}), ErrPublicStatusProbeConfigRequiresCAS)
			assert.Equal(t, rawBefore, requirePublicStatusProbeOptionRaw(t, db))
			var guarded Option
			require.NoError(t, db.Where("key = ?", "test.equivalent-bulk-guard").Take(&guarded).Error)
			assert.Equal(t, "before", guarded.Value)
		})
	}
}

func TestGenericOptionWritersRejectCanonicalAuthoritativeReadBeforeSave(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		t.Run(fmt.Sprintf("bulk=%t", bulk), func(t *testing.T) {
			db := usePublicStatusProbeConfigDB(t)
			useInitialPublicStatusProbeRuntime(t)
			_, _, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(7))
			require.NoError(t, err)
			rawBefore := requirePublicStatusProbeOptionRaw(t, db)
			const alias = "simulated-collation-alias"
			const sibling = "test.authoritative-read-guard"
			require.NoError(t, db.Create(&Option{Key: sibling, Value: "before"}).Error)
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:canonical-option-readback", func(tx *gorm.DB) {
				option, ok := tx.Statement.Dest.(*Option)
				if !ok || option.Key != alias {
					return
				}
				option.Key = publicstatusprobesetting.OptionKey
				option.Value = rawBefore
				tx.RowsAffected = 1
			}))

			if bulk {
				err = UpdateOptionsBulk(map[string]string{alias: "after", sibling: "after"})
			} else {
				err = UpdateOption(alias, "after")
			}
			require.ErrorIs(t, err, ErrPublicStatusProbeConfigRequiresCAS)
			assert.Equal(t, rawBefore, requirePublicStatusProbeOptionRaw(t, db))
			var guarded Option
			require.NoError(t, db.Where("key = ?", sibling).Take(&guarded).Error)
			assert.Equal(t, "before", guarded.Value)
			var aliases int64
			require.NoError(t, db.Model(&Option{}).Where("key = ?", alias).Count(&aliases).Error)
			assert.Zero(t, aliases)
		})
	}
}

func publicStatusProbeDocument(marker int) publicstatusprobesetting.Document {
	document := publicstatusprobesetting.DefaultDocument()
	document.Enabled = true
	document.Targets = []publicstatusprobesetting.Target{
		{
			Enabled:     true,
			Key:         fmt.Sprintf("target-%d", marker),
			Group:       "core",
			DisplayName: fmt.Sprintf("Target %d", marker),
			Model:       "gpt-5.5",
			Protocol:    publicstatusprobesetting.ProtocolOpenAIChat,
			ChannelID:   marker + 1,
			KeyIndex:    0,
		},
	}
	return document
}

func requireNoPublishedVersion(t *testing.T, versions <-chan int64) {
	t.Helper()
	select {
	case version := <-versions:
		t.Fatalf("unexpected published version %d", version)
	default:
	}
}

func requirePublishedVersion(t *testing.T, versions <-chan int64) int64 {
	t.Helper()
	select {
	case version := <-versions:
		return version
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published version")
		return 0
	}
}

func TestEnsurePublicStatusProbeConfigImportsOnlyWhenAbsent(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	bootstrap := publicStatusProbeDocument(1)

	saved, created, err := EnsurePublicStatusProbeConfig(bootstrap)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, bootstrap, saved)

	var option Option
	require.NoError(t, db.Where("key = ?", publicstatusprobesetting.OptionKey).First(&option).Error)
	persisted, err := publicstatusprobesetting.DecodeDocument(option.Value)
	require.NoError(t, err)
	assert.Equal(t, bootstrap, persisted)
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, option.Value, common.OptionMap[publicstatusprobesetting.OptionKey])
	common.OptionMapRWMutex.RUnlock()

	replacement := publicStatusProbeDocument(2)
	loaded, created, err := EnsurePublicStatusProbeConfig(replacement)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, bootstrap, loaded)
	assert.Equal(t, option.Value, requirePublicStatusProbeOptionRaw(t, db))
}

func TestEnsurePublicStatusProbeConfigKeepsExistingDisabledEmptyDocument(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	existing := publicstatusprobesetting.DefaultDocument()
	existing.Enabled = false
	existing.Targets = []publicstatusprobesetting.Target{}
	raw, err := publicstatusprobesetting.EncodeDocument(existing)
	require.NoError(t, err)
	require.NoError(t, db.Create(&Option{Key: publicstatusprobesetting.OptionKey, Value: raw}).Error)

	loaded, created, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(3))
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, existing, loaded)
	assert.Equal(t, raw, requirePublicStatusProbeOptionRaw(t, db))

	invalidBootstrap := publicStatusProbeDocument(4)
	invalidBootstrap.SchemaVersion++
	loaded, created, err = EnsurePublicStatusProbeConfig(invalidBootstrap)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, existing, loaded)
	assert.Equal(t, raw, requirePublicStatusProbeOptionRaw(t, db))
}

func TestEnsurePublicStatusProbeConfigConcurrentCreateHasOneWinner(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	const workers = 8
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("test:distort-create-rows", func(tx *gorm.DB) {
		tx.RowsAffected = 1
	}))

	type result struct {
		document publicstatusprobesetting.Document
		created  bool
		err      error
	}
	ready := make(chan struct{})
	results := make(chan result, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		group.Add(1)
		go func() {
			defer group.Done()
			<-ready
			document, created, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(worker + 10))
			results <- result{document: document, created: created, err: err}
		}()
	}
	close(ready)
	group.Wait()
	close(results)

	persistedRaw := requirePublicStatusProbeOptionRaw(t, db)
	persisted, err := publicstatusprobesetting.DecodeDocument(persistedRaw)
	require.NoError(t, err)
	winners := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.created {
			winners++
		}
		assert.Equal(t, persisted, result.document)
	}
	assert.Equal(t, 1, winners)

	var count int64
	require.NoError(t, db.Model(&Option{}).Where("key = ?", publicstatusprobesetting.OptionKey).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestCompareAndSwapPublicStatusProbeConfigRejectsStaleVersionWithoutPublish(t *testing.T) {
	usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	saved, _, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(20))
	require.NoError(t, err)
	require.NoError(t, publicstatusprobesetting.PublishDocument(saved))

	versions := make(chan int64, 1)
	publicstatusprobesetting.SetPublishHook(func(version int64) { versions <- version })
	_, err = CompareAndSwapPublicStatusProbeConfig(saved.Version+1, func(next *publicstatusprobesetting.Document) error {
		next.Enabled = false
		return nil
	})
	require.ErrorIs(t, err, ErrPublicStatusProbeConfigConflict)
	assert.Equal(t, saved, publicstatusprobesetting.CurrentDocument())
	requireNoPublishedVersion(t, versions)
}

func TestCompareAndSwapPublicStatusProbeConfigIncrementsOnceAndPublishes(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	saved, _, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(21))
	require.NoError(t, err)
	require.NoError(t, publicstatusprobesetting.PublishDocument(saved))

	versions := make(chan int64, 1)
	publicstatusprobesetting.SetPublishHook(func(version int64) { versions <- version })
	updated, err := CompareAndSwapPublicStatusProbeConfig(saved.Version, func(next *publicstatusprobesetting.Document) error {
		next.Enabled = false
		next.Version = saved.Version + 100
		return nil
	})
	require.NoError(t, err)
	assert.False(t, updated.Enabled)
	assert.Equal(t, saved.Version+1, updated.Version)
	assert.Equal(t, updated, publicstatusprobesetting.CurrentDocument())
	assert.Equal(t, updated.Version, requirePublishedVersion(t, versions))

	persisted, err := publicstatusprobesetting.DecodeDocument(requirePublicStatusProbeOptionRaw(t, db))
	require.NoError(t, err)
	assert.Equal(t, updated, persisted)
}

func TestCompareAndSwapPublicStatusProbeConfigRowsAffectedConflictDoesNotPublish(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	saved, _, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(22))
	require.NoError(t, err)
	require.NoError(t, publicstatusprobesetting.PublishDocument(saved))

	versions := make(chan int64, 1)
	publicstatusprobesetting.SetPublishHook(func(version int64) { versions <- version })
	winner := publicStatusProbeDocument(23)
	winner.Version = saved.Version + 1
	winnerRaw, err := publicstatusprobesetting.EncodeDocument(winner)
	require.NoError(t, err)

	_, err = CompareAndSwapPublicStatusProbeConfig(saved.Version, func(next *publicstatusprobesetting.Document) error {
		require.NoError(t, db.Model(&Option{}).
			Where("key = ?", publicstatusprobesetting.OptionKey).
			Update("value", winnerRaw).Error)
		next.Enabled = false
		return nil
	})
	require.ErrorIs(t, err, ErrPublicStatusProbeConfigConflict)
	assert.Equal(t, winnerRaw, requirePublicStatusProbeOptionRaw(t, db))
	assert.Equal(t, saved, publicstatusprobesetting.CurrentDocument())
	requireNoPublishedVersion(t, versions)
}

func TestCompareAndSwapPublicStatusProbeConfigWriteFailureDoesNotPublish(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	saved, _, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(24))
	require.NoError(t, err)
	require.NoError(t, publicstatusprobesetting.PublishDocument(saved))

	writeErr := errors.New("forced write failure")
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:public-probe-write-failure", func(tx *gorm.DB) {
		tx.AddError(writeErr)
	}))
	versions := make(chan int64, 1)
	publicstatusprobesetting.SetPublishHook(func(version int64) { versions <- version })

	_, err = CompareAndSwapPublicStatusProbeConfig(saved.Version, func(next *publicstatusprobesetting.Document) error {
		next.Enabled = false
		return nil
	})
	require.ErrorIs(t, err, writeErr)
	assert.Equal(t, saved, publicstatusprobesetting.CurrentDocument())
	requireNoPublishedVersion(t, versions)
}

func TestCompareAndSwapPublicStatusProbeConfigMutationFailureDoesNotWriteOrPublish(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	saved, _, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(25))
	require.NoError(t, err)
	require.NoError(t, publicstatusprobesetting.PublishDocument(saved))
	before := requirePublicStatusProbeOptionRaw(t, db)

	mutateErr := errors.New("mutation rejected")
	versions := make(chan int64, 1)
	publicstatusprobesetting.SetPublishHook(func(version int64) { versions <- version })
	_, err = CompareAndSwapPublicStatusProbeConfig(saved.Version, func(next *publicstatusprobesetting.Document) error {
		next.Enabled = false
		return mutateErr
	})
	require.ErrorIs(t, err, mutateErr)
	assert.Equal(t, before, requirePublicStatusProbeOptionRaw(t, db))
	assert.Equal(t, saved, publicstatusprobesetting.CurrentDocument())
	requireNoPublishedVersion(t, versions)
}

func TestUpdateOptionMapStrictlyAppliesPublicStatusProbeConfig(t *testing.T) {
	preservePublicStatusProbeRuntime(t)
	common.OptionMapRWMutex.Lock()
	previousMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousMap
		common.OptionMapRWMutex.Unlock()
	})

	document := publicStatusProbeDocument(30)
	document.Version = 7
	raw, err := publicstatusprobesetting.EncodeDocument(document)
	require.NoError(t, err)
	versions := make(chan int64, 1)
	publicstatusprobesetting.SetPublishHook(func(version int64) { versions <- version })

	require.NoError(t, updateOptionMap(publicstatusprobesetting.OptionKey, raw))
	assert.Equal(t, document, publicstatusprobesetting.CurrentDocument())
	assert.Equal(t, document.Version, requirePublishedVersion(t, versions))
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, raw, common.OptionMap[publicstatusprobesetting.OptionKey])
	common.OptionMapRWMutex.RUnlock()

	alias := strings.ToLower(publicstatusprobesetting.OptionKey)
	require.ErrorIs(t, updateOptionMap(alias, raw), ErrPublicStatusProbeConfigRequiresCAS)
	common.OptionMapRWMutex.RLock()
	_, aliasExists := common.OptionMap[alias]
	assert.False(t, aliasExists)
	common.OptionMapRWMutex.RUnlock()

	invalid := `{"schema_version":1,"version":8,"enabled":true,"ping_timeout_seconds":8,"chat_timeout_seconds":45,"degraded_latency_ms":6000,"concurrency":5,"retention_days":7,"targets":[],"secret":"hidden"}`
	require.Error(t, validateOptionValue(publicstatusprobesetting.OptionKey, invalid))
	require.Error(t, updateOptionMap(publicstatusprobesetting.OptionKey, invalid))
	assert.Equal(t, document, publicstatusprobesetting.CurrentDocument())
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, raw, common.OptionMap[publicstatusprobesetting.OptionKey])
	common.OptionMapRWMutex.RUnlock()
	requireNoPublishedVersion(t, versions)
}

func TestApplyPublicStatusProbeConfigOptionIsMonotonicAndIdempotent(t *testing.T) {
	preservePublicStatusProbeRuntime(t)
	current := publicStatusProbeDocument(31)
	current.Version = 7
	currentRaw, err := publicstatusprobesetting.EncodeDocument(current)
	require.NoError(t, err)
	setPublicStatusProbeOptionMap(currentRaw)
	require.NoError(t, publicstatusprobesetting.PublishDocument(current))

	versions := make(chan int64, 1)
	publicstatusprobesetting.SetPublishHook(func(version int64) { versions <- version })
	require.NoError(t, ApplyPublicStatusProbeConfigOption(currentRaw))
	requireNoPublishedVersion(t, versions)

	stale := publicStatusProbeDocument(32)
	stale.Version = current.Version - 1
	staleRaw, err := publicstatusprobesetting.EncodeDocument(stale)
	require.NoError(t, err)
	require.NoError(t, ApplyPublicStatusProbeConfigOption(staleRaw))
	assert.Equal(t, current, publicstatusprobesetting.CurrentDocument())
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, currentRaw, common.OptionMap[publicstatusprobesetting.OptionKey])
	common.OptionMapRWMutex.RUnlock()
	requireNoPublishedVersion(t, versions)

	conflicting := publicStatusProbeDocument(33)
	conflicting.Version = current.Version
	conflictingRaw, err := publicstatusprobesetting.EncodeDocument(conflicting)
	require.NoError(t, err)
	publicStatusProbeConfigLoaded.Store(true)
	common.OptionMapRWMutex.Lock()
	delete(common.OptionMap, publicstatusprobesetting.OptionKey)
	common.OptionMapRWMutex.Unlock()
	require.ErrorIs(t, ApplyPublicStatusProbeConfigOption(conflictingRaw), ErrPublicStatusProbeConfigConflict)
	assert.Equal(t, current, publicstatusprobesetting.CurrentDocument())
	requireNoPublishedVersion(t, versions)
}

func TestApplyPublicStatusProbeConfigOptionAcceptsInitialVersionOnce(t *testing.T) {
	useInitialPublicStatusProbeRuntime(t)
	initial := publicStatusProbeDocument(33)
	initial.Version = publicstatusprobesetting.DefaultDocument().Version
	initialRaw, err := publicstatusprobesetting.EncodeDocument(initial)
	require.NoError(t, err)
	require.NoError(t, ApplyPublicStatusProbeConfigOption(initialRaw))
	assert.Equal(t, initial, publicstatusprobesetting.CurrentDocument())

	conflicting := publicStatusProbeDocument(34)
	conflicting.Version = initial.Version
	conflictingRaw, err := publicstatusprobesetting.EncodeDocument(conflicting)
	require.NoError(t, err)
	require.ErrorIs(t, ApplyPublicStatusProbeConfigOption(conflictingRaw), ErrPublicStatusProbeConfigConflict)
	assert.Equal(t, initial, publicstatusprobesetting.CurrentDocument())
}

func TestUpdateOptionMapPublishesPublicStatusProbeConfigWithoutHoldingOptionMapLock(t *testing.T) {
	preservePublicStatusProbeRuntime(t)
	current := publicStatusProbeDocument(34)
	current.Version = 10
	currentRaw, err := publicstatusprobesetting.EncodeDocument(current)
	require.NoError(t, err)
	setPublicStatusProbeOptionMap(currentRaw)
	require.NoError(t, publicstatusprobesetting.PublishDocument(current))

	next := publicStatusProbeDocument(35)
	next.Version = current.Version + 1
	nextRaw, err := publicstatusprobesetting.EncodeDocument(next)
	require.NoError(t, err)
	hookDone := make(chan struct{}, 1)
	publicstatusprobesetting.SetPublishHook(func(version int64) {
		common.OptionMapRWMutex.RLock()
		assert.Equal(t, nextRaw, common.OptionMap[publicstatusprobesetting.OptionKey])
		common.OptionMapRWMutex.RUnlock()
		hookDone <- struct{}{}
	})

	applyDone := make(chan error, 1)
	go func() { applyDone <- updateOptionMap(publicstatusprobesetting.OptionKey, nextRaw) }()
	select {
	case err := <-applyDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("updateOptionMap deadlocked while publishing")
	}
	select {
	case <-hookDone:
	case <-time.After(time.Second):
		t.Fatal("publish hook was not called")
	}
}

func TestPublicStatusProbeHookCanReenterApplyWithoutDeadlockOrLostNotification(t *testing.T) {
	preservePublicStatusProbeRuntime(t)
	current := publicStatusProbeDocument(36)
	current.Version = 1
	currentRaw, err := publicstatusprobesetting.EncodeDocument(current)
	require.NoError(t, err)
	setPublicStatusProbeOptionMap(currentRaw)
	require.NoError(t, publicstatusprobesetting.PublishDocument(current))

	second := publicStatusProbeDocument(37)
	second.Version = 2
	secondRaw, err := publicstatusprobesetting.EncodeDocument(second)
	require.NoError(t, err)
	third := publicStatusProbeDocument(38)
	third.Version = 3
	thirdRaw, err := publicstatusprobesetting.EncodeDocument(third)
	require.NoError(t, err)

	versions := make(chan int64, 2)
	reentrant := make(chan error, 1)
	publicstatusprobesetting.SetPublishHook(func(version int64) {
		versions <- version
		if version == second.Version {
			reentrant <- ApplyPublicStatusProbeConfigOption(thirdRaw)
		}
	})
	done := make(chan error, 1)
	go func() { done <- ApplyPublicStatusProbeConfigOption(secondRaw) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Apply deadlocked when its hook reentered Apply")
	}
	select {
	case err := <-reentrant:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reentrant Apply")
	}
	assert.Equal(t, int64(2), requirePublishedVersion(t, versions))
	assert.Equal(t, int64(3), requirePublishedVersion(t, versions))
	assert.Equal(t, third, publicstatusprobesetting.CurrentDocument())
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, thirdRaw, common.OptionMap[publicstatusprobesetting.OptionKey])
	common.OptionMapRWMutex.RUnlock()
}

func TestPublicStatusProbeHookCanReenterCASWithoutDeadlockOrVersionRegression(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	saved, _, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(39))
	require.NoError(t, err)
	require.NoError(t, publicstatusprobesetting.PublishDocument(saved))

	versions := make(chan int64, 2)
	reentrant := make(chan error, 1)
	publicstatusprobesetting.SetPublishHook(func(version int64) {
		versions <- version
		if version == saved.Version+1 {
			_, err := CompareAndSwapPublicStatusProbeConfig(version, func(next *publicstatusprobesetting.Document) error {
				next.RetentionDays++
				return nil
			})
			reentrant <- err
		}
	})
	done := make(chan error, 1)
	go func() {
		_, err := CompareAndSwapPublicStatusProbeConfig(saved.Version, func(next *publicstatusprobesetting.Document) error {
			next.Enabled = false
			return nil
		})
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("CAS deadlocked when its hook reentered CAS")
	}
	select {
	case err := <-reentrant:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reentrant CAS")
	}
	assert.Equal(t, saved.Version+1, requirePublishedVersion(t, versions))
	assert.Equal(t, saved.Version+2, requirePublishedVersion(t, versions))
	assert.Equal(t, saved.Version+2, publicstatusprobesetting.CurrentDocument().Version)
	persisted, err := publicstatusprobesetting.DecodeDocument(requirePublicStatusProbeOptionRaw(t, db))
	require.NoError(t, err)
	assert.Equal(t, saved.Version+2, persisted.Version)
	optionRaw, err := publicstatusprobesetting.EncodeDocument(persisted)
	require.NoError(t, err)
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, optionRaw, common.OptionMap[publicstatusprobesetting.OptionKey])
	common.OptionMapRWMutex.RUnlock()
}

func TestPublicStatusProbeDBOperationsNeverLogRawJSON(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	var output bytes.Buffer
	noisy := logger.New(log.New(&output, "", 0), logger.Config{
		LogLevel:             logger.Info,
		ParameterizedQueries: false,
	})
	DB = db.Session(&gorm.Session{Logger: noisy})

	document := publicStatusProbeDocument(41)
	document.Targets[0].DisplayName = "raw-json-marker"
	saved, _, err := EnsurePublicStatusProbeConfig(document)
	require.NoError(t, err)
	_, err = GetPublicStatusProbeConfig()
	require.NoError(t, err)
	_, err = CompareAndSwapPublicStatusProbeConfig(saved.Version, func(next *publicstatusprobesetting.Document) error {
		next.Enabled = false
		return nil
	})
	require.NoError(t, err)
	assert.NotContains(t, output.String(), "raw-json-marker")
}

func TestPublicStatusProbeSilentSessionPreservesTransactionRollback(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	document := publicStatusProbeDocument(42)
	raw, err := publicstatusprobesetting.EncodeDocument(document)
	require.NoError(t, err)
	rollback := errors.New("rollback public status probe create")

	err = db.Transaction(func(tx *gorm.DB) error {
		result := createPublicStatusProbeConfig(tx, &Option{
			Key:   publicstatusprobesetting.OptionKey,
			Value: raw,
		})
		require.NoError(t, result.Error)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	var count int64
	require.NoError(t, db.Model(&Option{}).Where("key = ?", publicstatusprobesetting.OptionKey).Count(&count).Error)
	assert.Zero(t, count)
}

func TestPublicStatusProbeDatabaseIdentityIgnoresUnicodeAlias(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	aliasDocument := publicStatusProbeDocument(43)
	aliasDocument.Version = 9
	aliasRaw, err := publicstatusprobesetting.EncodeDocument(aliasDocument)
	require.NoError(t, err)
	const aliasKey = "PúblicStatusProbeConfig"
	require.NoError(t, db.Create(&Option{Key: aliasKey, Value: aliasRaw}).Error)

	_, err = GetPublicStatusProbeConfig()
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.ErrorIs(t, updateOptionMap(aliasKey, aliasRaw), ErrPublicStatusProbeConfigRequiresCAS)
	assert.Equal(t, publicstatusprobesetting.DefaultDocument(), publicstatusprobesetting.CurrentDocument())
	common.OptionMapRWMutex.RLock()
	_, canonicalExists := common.OptionMap[publicstatusprobesetting.OptionKey]
	_, aliasExists := common.OptionMap[aliasKey]
	common.OptionMapRWMutex.RUnlock()
	assert.False(t, canonicalExists)
	assert.False(t, aliasExists)

	bootstrap := publicStatusProbeDocument(44)
	saved, created, err := EnsurePublicStatusProbeConfig(bootstrap)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, bootstrap, saved)
	assert.Equal(t, bootstrap, publicstatusprobesetting.CurrentDocument())
	var aliases int64
	require.NoError(t, db.Model(&Option{}).Where("key = ?", aliasKey).Count(&aliases).Error)
	assert.Equal(t, int64(1), aliases)
}

func TestEnsurePublicStatusProbeConfigReturnsIdentityConflictWhenCanonicalCreateIsIgnored(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
	useInitialPublicStatusProbeRuntime(t)
	aliasDocument := publicStatusProbeDocument(45)
	aliasRaw, err := publicstatusprobesetting.EncodeDocument(aliasDocument)
	require.NoError(t, err)
	require.NoError(t, db.Create(&Option{Key: "PúblicStatusProbeConfig", Value: aliasRaw}).Error)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER ignore_public_status_probe_canonical_create
		BEFORE INSERT ON options
		WHEN NEW.key = 'PublicStatusProbeConfig'
		BEGIN
			SELECT RAISE(IGNORE);
		END
	`).Error)

	_, created, err := EnsurePublicStatusProbeConfig(publicStatusProbeDocument(46))
	require.ErrorIs(t, err, ErrPublicStatusProbeConfigIdentityConflict)
	assert.False(t, created)
	assert.Equal(t, publicstatusprobesetting.DefaultDocument(), publicstatusprobesetting.CurrentDocument())
	common.OptionMapRWMutex.RLock()
	_, canonicalExists := common.OptionMap[publicstatusprobesetting.OptionKey]
	common.OptionMapRWMutex.RUnlock()
	assert.False(t, canonicalExists)
}

func TestPublicStatusProbePersistenceSQLIsPortable(t *testing.T) {
	bootstrap := publicStatusProbeDocument(40)
	newRaw, err := publicstatusprobesetting.EncodeDocument(bootstrap)
	require.NoError(t, err)
	oldRaw := strings.Replace(newRaw, `"enabled":true`, `"enabled":false`, 1)
	require.NotEqual(t, newRaw, oldRaw)

	dialects := map[string]gorm.Dialector{
		"mysql": mysql.New(mysql.Config{
			DSN:                       "user:pass@tcp(localhost:3306)/newapi?charset=utf8mb4&parseTime=True&loc=Local",
			SkipInitializeWithVersion: true,
		}),
		"postgresql": postgres.New(postgres.Config{
			DSN:                  "host=localhost user=user password=pass dbname=newapi port=5432 sslmode=disable",
			PreferSimpleProtocol: true,
		}),
	}
	for name, dialector := range dialects {
		t.Run(name, func(t *testing.T) {
			db, err := gorm.Open(dialector, &gorm.Config{
				DryRun:                 true,
				DisableAutomaticPing:   true,
				SkipDefaultTransaction: true,
			})
			require.NoError(t, err)

			create := createPublicStatusProbeConfig(db, &Option{
				Key:   publicstatusprobesetting.OptionKey,
				Value: oldRaw,
			})
			require.NoError(t, create.Error)
			createSQL := strings.ToUpper(create.Statement.SQL.String())
			assert.Contains(t, createSQL, "INSERT")
			if name == "mysql" {
				assert.Contains(t, createSQL, "ON DUPLICATE KEY")
			} else {
				assert.Contains(t, createSQL, "ON CONFLICT DO NOTHING")
			}

			var option Option
			lookup := takePublicStatusProbeOption(db, &option)
			require.NoError(t, lookup.Error)
			lookupSQL := strings.ToUpper(lookup.Statement.SQL.String())
			if name == "mysql" {
				assert.Contains(t, lookupSQL, "BINARY `KEY` = BINARY")
			} else {
				assert.Contains(t, lookupSQL, `"KEY" =`)
			}
			assert.Contains(t, lookup.Statement.Vars, publicstatusprobesetting.OptionKey)

			update := updatePublicStatusProbeConfig(db, oldRaw, newRaw)
			require.NoError(t, update.Error)
			updateSQL := strings.ToUpper(update.Statement.SQL.String())
			assert.Contains(t, updateSQL, "UPDATE")
			assert.Contains(t, updateSQL, "WHERE")
			assert.Contains(t, updateSQL, "KEY")
			assert.Contains(t, updateSQL, "VALUE")
			if name == "mysql" {
				assert.Contains(t, updateSQL, "BINARY `KEY` = BINARY")
				assert.Contains(t, updateSQL, "BINARY `VALUE` = BINARY")
			} else {
				assert.Contains(t, updateSQL, `"KEY"`)
				assert.Contains(t, updateSQL, `"VALUE"`)
			}
			assert.Contains(t, update.Statement.Vars, publicstatusprobesetting.OptionKey)
			assert.Contains(t, update.Statement.Vars, oldRaw)
			assert.Contains(t, update.Statement.Vars, newRaw)
		})
	}
}

func requirePublicStatusProbeOptionRaw(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var option Option
	require.NoError(t, db.Where("key = ?", publicstatusprobesetting.OptionKey).First(&option).Error)
	return option.Value
}
