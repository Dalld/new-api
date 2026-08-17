package model

import (
	"errors"
	"fmt"
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
	common.OptionMapRWMutex.Lock()
	previousMap := common.OptionMap
	testMap := make(map[string]string, len(previousMap))
	for key, value := range previousMap {
		testMap[key] = value
	}
	common.OptionMap = testMap
	common.OptionMapRWMutex.Unlock()
	publicstatusprobesetting.SetPublishHook(nil)
	t.Cleanup(func() {
		publicstatusprobesetting.SetPublishHook(nil)
		require.NoError(t, publicstatusprobesetting.PublishDocument(previous))
		publicstatusprobesetting.SetPublishHook(nil)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousMap
		common.OptionMapRWMutex.Unlock()
	})
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

	replacement := publicStatusProbeDocument(2)
	loaded, created, err := EnsurePublicStatusProbeConfig(replacement)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, bootstrap, loaded)
	assert.Equal(t, option.Value, requirePublicStatusProbeOptionRaw(t, db))
}

func TestEnsurePublicStatusProbeConfigKeepsExistingDisabledEmptyDocument(t *testing.T) {
	db := usePublicStatusProbeConfigDB(t)
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
	preservePublicStatusProbeRuntime(t)
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
	preservePublicStatusProbeRuntime(t)
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
	preservePublicStatusProbeRuntime(t)
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
	preservePublicStatusProbeRuntime(t)
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
	preservePublicStatusProbeRuntime(t)
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
	common.OptionMapRWMutex.Lock()
	delete(common.OptionMap, publicstatusprobesetting.OptionKey)
	common.OptionMapRWMutex.Unlock()
	require.ErrorIs(t, ApplyPublicStatusProbeConfigOption(conflictingRaw), ErrPublicStatusProbeConfigConflict)
	assert.Equal(t, current, publicstatusprobesetting.CurrentDocument())
	requireNoPublishedVersion(t, versions)
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

			update := updatePublicStatusProbeConfig(db, oldRaw, newRaw)
			require.NoError(t, update.Error)
			updateSQL := strings.ToUpper(update.Statement.SQL.String())
			assert.Contains(t, updateSQL, "UPDATE")
			assert.Contains(t, updateSQL, "WHERE")
			assert.Contains(t, updateSQL, "KEY")
			assert.Contains(t, updateSQL, "VALUE")
			if name == "mysql" {
				assert.Contains(t, updateSQL, "`KEY`")
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
