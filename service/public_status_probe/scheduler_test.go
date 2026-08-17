package public_status_probe

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type targetLoaderFunc func(context.Context, publicstatusprobesetting.Target) (LoadedTarget, error)

func (function targetLoaderFunc) Load(ctx context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
	return function(ctx, target)
}

type fakeSettingProvider struct {
	mutex   sync.Mutex
	setting publicstatusprobesetting.Setting
	calls   int
}

func (provider *fakeSettingProvider) CurrentSetting() publicstatusprobesetting.Setting {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.calls++
	return cloneTestSetting(provider.setting)
}

func (provider *fakeSettingProvider) set(setting publicstatusprobesetting.Setting) {
	provider.mutex.Lock()
	provider.setting = cloneTestSetting(setting)
	provider.mutex.Unlock()
}

func (provider *fakeSettingProvider) callCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.calls
}

func cloneTestSetting(setting publicstatusprobesetting.Setting) publicstatusprobesetting.Setting {
	setting.Targets = append([]publicstatusprobesetting.Target(nil), setting.Targets...)
	return setting
}

type fakeLease struct {
	owner string
	until int64
}

type fakeProbeRepository struct {
	mutex       sync.Mutex
	results     map[string]*model.PublicStatusProbeResult
	leases      map[string]fakeLease
	created     []*model.PublicStatusProbeResult
	deleteCalls []int64
	renewCalls  int
}

func newFakeProbeRepository() *fakeProbeRepository {
	return &fakeProbeRepository{
		results: make(map[string]*model.PublicStatusProbeResult),
		leases:  make(map[string]fakeLease),
	}
}

func resultSlotKey(targetKey string, slot int64) string {
	return targetKey + "/" + time.Unix(slot, 0).UTC().Format(time.RFC3339)
}

func (repository *fakeProbeRepository) ResultExists(targetKey string, slotStartedAt int64) (bool, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	_, exists := repository.results[resultSlotKey(targetKey, slotStartedAt)]
	return exists, nil
}

func (repository *fakeProbeRepository) AcquireLease(targetKey, ownerID string, now, leaseUntil int64) (bool, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	lease := repository.leases[targetKey]
	if lease.until > now && lease.owner != ownerID {
		return false, nil
	}
	repository.leases[targetKey] = fakeLease{owner: ownerID, until: leaseUntil}
	return true, nil
}

func (repository *fakeProbeRepository) RenewLease(targetKey, ownerID string, now, leaseUntil int64) (bool, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	lease := repository.leases[targetKey]
	if lease.owner != ownerID || lease.until <= now {
		return false, nil
	}
	repository.leases[targetKey] = fakeLease{owner: ownerID, until: leaseUntil}
	repository.renewCalls++
	return true, nil
}

func (repository *fakeProbeRepository) CompleteLease(_ context.Context, targetKey, ownerID string, now, holdUntil int64) (bool, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	lease := repository.leases[targetKey]
	if lease.owner != ownerID || lease.until <= now {
		return false, nil
	}
	repository.leases[targetKey] = fakeLease{until: holdUntil}
	return true, nil
}

func (repository *fakeProbeRepository) CreateResult(_ context.Context, result *model.PublicStatusProbeResult, ownerID string, now int64) (bool, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	lease := repository.leases[result.TargetKey]
	if lease.owner != ownerID || lease.until <= now {
		return false, nil
	}
	key := resultSlotKey(result.TargetKey, result.SlotStartedAt)
	if _, exists := repository.results[key]; exists {
		return false, nil
	}
	copy := *result
	repository.results[key] = &copy
	repository.created = append(repository.created, &copy)
	return true, nil
}

func (repository *fakeProbeRepository) DeleteBefore(cutoff int64) (int64, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	repository.deleteCalls = append(repository.deleteCalls, cutoff)
	return 0, nil
}

func schedulerSetting(targetCount, concurrency int) publicstatusprobesetting.Setting {
	targets := make([]publicstatusprobesetting.Target, 0, targetCount)
	for index := 0; index < targetCount; index++ {
		targets = append(targets, publicstatusprobesetting.Target{
			Key:         "target-" + string(rune('a'+index)),
			Group:       "default",
			DisplayName: "Target",
			Model:       "model",
			Protocol:    publicstatusprobesetting.ProtocolOpenAIChat,
			ChannelID:   index + 1,
		})
	}
	return publicstatusprobesetting.Setting{
		Enabled:         true,
		Interval:        time.Minute,
		PingTimeout:     time.Second,
		ChatTimeout:     time.Second,
		DegradedLatency: 500 * time.Millisecond,
		Concurrency:     concurrency,
		RetentionDays:   7,
		Targets:         targets,
	}
}

func loadedSchedulerTarget(target publicstatusprobesetting.Target) LoadedTarget {
	return LoadedTarget{
		TargetKey:   target.Key,
		Group:       target.Group,
		DisplayName: target.DisplayName,
		ModelName:   target.Model,
		ChannelID:   target.ChannelID,
		Snapshot: Snapshot{
			Protocol: ProtocolOpenAIChat,
			BaseURL:  "https://provider.example",
			APIKey:   "provider-key",
			Model:    target.Model,
		},
	}
}

func newTestScheduler(t *testing.T, setting publicstatusprobesetting.Setting, loader TargetLoader, repository probeRepository) *Scheduler {
	t.Helper()
	provider := &fakeSettingProvider{setting: cloneTestSetting(setting)}
	scheduler, err := NewScheduler(provider, loader, repository, nil)
	require.NoError(t, err)
	scheduler.adapterFor = func(Snapshot, *http.Client) (Adapter, error) {
		return adapterFunc(func(context.Context, Snapshot, Challenge) (string, error) { return "unused", nil }), nil
	}
	scheduler.ping = func(context.Context, *http.Client, string, time.Duration) PingResult {
		latency := int64(10)
		return PingResult{Reachable: true, LatencyMS: &latency}
	}
	scheduler.runConversation = func(context.Context, Adapter, Snapshot, time.Duration, time.Duration) ConversationResult {
		latency := int64(20)
		return ConversationResult{State: StateOperational, LatencyMS: &latency}
	}
	return scheduler
}

func newTestSchedulerWithProvider(t *testing.T, provider SettingProvider, loader TargetLoader, repository probeRepository) *Scheduler {
	t.Helper()
	scheduler, err := NewScheduler(provider, loader, repository, nil)
	require.NoError(t, err)
	scheduler.adapterFor = func(Snapshot, *http.Client) (Adapter, error) {
		return adapterFunc(func(context.Context, Snapshot, Challenge) (string, error) { return "unused", nil }), nil
	}
	scheduler.ping = func(context.Context, *http.Client, string, time.Duration) PingResult {
		latency := int64(10)
		return PingResult{Reachable: true, LatencyMS: &latency}
	}
	scheduler.runConversation = func(context.Context, Adapter, Snapshot, time.Duration, time.Duration) ConversationResult {
		latency := int64(20)
		return ConversationResult{State: StateOperational, LatencyMS: &latency}
	}
	return scheduler
}

func TestSchedulerReadsOneSettingSnapshotPerSlot(t *testing.T) {
	first := schedulerSetting(1, 1)
	second := schedulerSetting(1, 1)
	second.Targets[0].Key = "target-next-slot"
	second.PingTimeout = 2 * time.Second
	second.ChatTimeout = 3 * time.Second
	second.DegradedLatency = 750 * time.Millisecond
	provider := &fakeSettingProvider{setting: first}
	repository := newFakeProbeRepository()
	entered := make(chan struct{})
	release := make(chan struct{})
	loader := targetLoaderFunc(func(_ context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
		close(entered)
		<-release
		return loadedSchedulerTarget(target), nil
	})
	scheduler := newTestSchedulerWithProvider(t, provider, loader, repository)
	var pingTimeout time.Duration
	var chatTimeout time.Duration
	var degradedLatency time.Duration
	scheduler.ping = func(_ context.Context, _ *http.Client, _ string, timeout time.Duration) PingResult {
		pingTimeout = timeout
		latency := int64(10)
		return PingResult{Reachable: true, LatencyMS: &latency}
	}
	scheduler.runConversation = func(_ context.Context, _ Adapter, _ Snapshot, timeout, degraded time.Duration) ConversationResult {
		chatTimeout = timeout
		degradedLatency = degraded
		latency := int64(20)
		return ConversationResult{State: StateOperational, LatencyMS: &latency}
	}

	done := make(chan bool, 1)
	go func() { done <- scheduler.runSlot(context.Background(), time.Unix(1_700_000_040, 0)) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("slot did not enter loader within one second")
	}
	provider.set(second)
	close(release)
	select {
	case completed := <-done:
		require.True(t, completed)
	case <-time.After(time.Second):
		t.Fatal("slot did not complete within one second")
	}

	assert.Equal(t, 2, provider.callCount(), "constructor plus one slot snapshot")
	require.Len(t, repository.created, 1)
	assert.Equal(t, "target-a", repository.created[0].TargetKey)
	assert.Equal(t, first.PingTimeout, pingTimeout)
	assert.Equal(t, first.ChatTimeout, chatTimeout)
	assert.Equal(t, first.DegradedLatency, degradedLatency)
}

func TestSchedulerAppliesSettingChangesOnNextSlot(t *testing.T) {
	first := schedulerSetting(1, 1)
	second := schedulerSetting(1, 1)
	second.Targets[0].Key = "target-next-slot"
	provider := &fakeSettingProvider{setting: first}
	repository := newFakeProbeRepository()
	loader := targetLoaderFunc(func(_ context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
		return loadedSchedulerTarget(target), nil
	})
	scheduler := newTestSchedulerWithProvider(t, provider, loader, repository)

	assert.True(t, scheduler.runSlot(context.Background(), time.Unix(1_700_000_040, 0)))
	provider.set(second)
	assert.True(t, scheduler.runSlot(context.Background(), time.Unix(1_700_000_100, 0)))

	require.Len(t, repository.created, 2)
	assert.Equal(t, "target-a", repository.created[0].TargetKey)
	assert.Equal(t, "target-next-slot", repository.created[1].TargetKey)
}

func TestSchedulerKeepsRunningAndRetainsWhileDisabledOrEmpty(t *testing.T) {
	setting := schedulerSetting(0, 1)
	setting.Enabled = false
	provider := &fakeSettingProvider{setting: setting}
	repository := newFakeProbeRepository()
	loaderCalls := 0
	loader := targetLoaderFunc(func(_ context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
		loaderCalls++
		return loadedSchedulerTarget(target), nil
	})
	scheduler := newTestSchedulerWithProvider(t, provider, loader, repository)

	assert.True(t, scheduler.runSlot(context.Background(), time.Unix(1_700_000_040, 0)))
	require.Len(t, repository.deleteCalls, 1)
	assert.Equal(t, 0, loaderCalls)

	setting.Enabled = true
	setting.Targets = []publicstatusprobesetting.Target{schedulerSetting(1, 1).Targets[0]}
	provider.set(setting)
	assert.True(t, scheduler.runSlot(context.Background(), time.Unix(1_700_000_100, 0)))
	assert.Equal(t, 1, loaderCalls)

	setting.Targets = nil
	setting.RetentionDays = 3
	provider.set(setting)
	emptySlot := time.Unix(1_700_000_040, 0).Add(12 * time.Hour)
	assert.True(t, scheduler.runSlot(context.Background(), emptySlot))
	assert.Equal(t, 1, loaderCalls)
	require.Len(t, repository.deleteCalls, 2)
	assert.Equal(t, emptySlot.Add(-3*24*time.Hour).Unix(), repository.deleteCalls[1])
}

func TestStartKeepsDisabledSchedulerAlive(t *testing.T) {
	setting := schedulerSetting(0, 1)
	setting.Enabled = false
	provider := &fakeSettingProvider{setting: setting}
	ctx, cancel := context.WithCancel(context.Background())
	done := Start(ctx, provider)

	select {
	case <-done:
		t.Fatal("disabled scheduler stopped before cancellation")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disabled scheduler did not stop after cancellation")
	}
}

func TestRuntimeSettingProviderFiltersDisabledTargets(t *testing.T) {
	previous := publicstatusprobesetting.CurrentDocument()
	t.Cleanup(func() {
		require.NoError(t, publicstatusprobesetting.PublishDocument(previous))
	})
	document := publicstatusprobesetting.DefaultDocument()
	document.Enabled = true
	document.Targets = schedulerSetting(2, 1).Targets
	document.Targets[0].Enabled = true
	document.Targets[1].Enabled = false
	require.NoError(t, publicstatusprobesetting.PublishDocument(document))

	setting := (RuntimeSettingProvider{}).CurrentSetting()
	require.Len(t, setting.Targets, 1)
	assert.Equal(t, "target-a", setting.Targets[0].Key)
}

func TestSchedulerRacingInstancesProbeOneTargetSlotOnce(t *testing.T) {
	setting := schedulerSetting(1, 1)
	repository := newFakeProbeRepository()
	var calls atomic.Int32
	loader := targetLoaderFunc(func(_ context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return loadedSchedulerTarget(target), nil
	})
	first := newTestScheduler(t, setting, loader, repository)
	second := newTestScheduler(t, setting, loader, repository)
	fixedNow := time.Unix(1_700_000_050, 0).UTC()
	first.now = func() time.Time { return fixedNow }
	second.now = first.now
	slot := time.Unix(1_700_000_040, 0).UTC()

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() { defer waitGroup.Done(); first.runSlot(context.Background(), slot) }()
	go func() { defer waitGroup.Done(); second.runSlot(context.Background(), slot) }()
	waitGroup.Wait()

	assert.EqualValues(t, 1, calls.Load())
	require.Len(t, repository.created, 1)
	assert.Equal(t, model.PublicStatusProbeStateOperational, repository.created[0].State)
	assert.Equal(t, int64(10), *repository.created[0].PingLatencyMS)
	assert.Equal(t, int64(20), *repository.created[0].ChatLatencyMS)
	assert.Empty(t, repository.created[0].ErrorCode)
	assert.Empty(t, repository.leases[setting.Targets[0].Key].owner)
	assert.Equal(t, nextProbeSlot(fixedNow, time.Minute).Unix(), repository.leases[setting.Targets[0].Key].until)
}

func TestSchedulerSkipsOverlappingLocalCycle(t *testing.T) {
	setting := schedulerSetting(1, 1)
	repository := newFakeProbeRepository()
	entered := make(chan struct{})
	release := make(chan struct{})
	loader := targetLoaderFunc(func(_ context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
		close(entered)
		<-release
		return loadedSchedulerTarget(target), nil
	})
	scheduler := newTestScheduler(t, setting, loader, repository)
	scheduler.now = func() time.Time { return time.Unix(1_700_000_050, 0).UTC() }
	slot := time.Unix(1_700_000_040, 0).UTC()
	done := make(chan bool, 1)
	go func() { done <- scheduler.runSlot(context.Background(), slot) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("slot did not enter loader within one second")
	}

	assert.False(t, scheduler.runSlot(context.Background(), slot.Add(time.Minute)))
	close(release)
	select {
	case completed := <-done:
		assert.True(t, completed)
	case <-time.After(time.Second):
		t.Fatal("slot did not complete within one second")
	}
}

func TestSchedulerRenewsLeaseWhileLoaderIsBlocked(t *testing.T) {
	setting := schedulerSetting(1, 1)
	repository := newFakeProbeRepository()
	entered := make(chan struct{})
	release := make(chan struct{})
	loader := targetLoaderFunc(func(ctx context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
		close(entered)
		select {
		case <-release:
			return loadedSchedulerTarget(target), nil
		case <-ctx.Done():
			return LoadedTarget{}, ctx.Err()
		}
	})
	scheduler := newTestScheduler(t, setting, loader, repository)
	base := time.Unix(1_700_000_050, 0).UTC()
	var clockCalls atomic.Int64
	scheduler.now = func() time.Time { return base.Add(time.Duration(clockCalls.Add(1)) * time.Second) }
	scheduler.renewInterval = 5 * time.Millisecond
	done := make(chan bool, 1)
	go func() { done <- scheduler.runSlot(context.Background(), time.Unix(1_700_000_040, 0)) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("slot did not enter loader within one second")
	}

	require.Eventually(t, func() bool {
		repository.mutex.Lock()
		defer repository.mutex.Unlock()
		return repository.renewCalls > 0
	}, time.Second, 5*time.Millisecond)
	close(release)
	select {
	case completed := <-done:
		assert.True(t, completed)
	case <-time.After(time.Second):
		t.Fatal("slot did not complete within one second")
	}
	require.Len(t, repository.created, 1)
}

func TestSchedulerBoundsTargetConcurrencyAndKeepsSiblingsIndependent(t *testing.T) {
	setting := schedulerSetting(5, 2)
	repository := newFakeProbeRepository()
	var active atomic.Int32
	var maximum atomic.Int32
	loader := targetLoaderFunc(func(_ context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		if target.Key == "target-c" {
			return LoadedTarget{}, codedError(ErrorUnsupportedProvider)
		}
		return loadedSchedulerTarget(target), nil
	})
	scheduler := newTestScheduler(t, setting, loader, repository)
	scheduler.now = func() time.Time { return time.Unix(1_700_000_050, 0).UTC() }

	assert.True(t, scheduler.runSlot(context.Background(), time.Unix(1_700_000_040, 0)))

	assert.LessOrEqual(t, maximum.Load(), int32(setting.Concurrency))
	require.Len(t, repository.created, len(setting.Targets))
	failures := 0
	for _, result := range repository.created {
		if result.TargetKey == "target-c" {
			failures++
			assert.Equal(t, model.PublicStatusProbeStateFailed, result.State)
			assert.Equal(t, string(ErrorUnsupportedProvider), result.ErrorCode)
		}
	}
	assert.Equal(t, 1, failures)
}

func TestSchedulerRetentionRunsAtMostEveryTwelveHours(t *testing.T) {
	setting := schedulerSetting(0, 1)
	repository := newFakeProbeRepository()
	scheduler := newTestScheduler(t, setting, targetLoaderFunc(func(_ context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
		return loadedSchedulerTarget(target), nil
	}), repository)
	base := time.Unix(1_700_000_040, 0).UTC()

	scheduler.runRetention(base, setting.RetentionDays)
	scheduler.runRetention(base.Add(11*time.Hour+59*time.Minute), setting.RetentionDays)
	scheduler.runRetention(base.Add(12*time.Hour), setting.RetentionDays)

	require.Len(t, repository.deleteCalls, 2)
	assert.Equal(t, base.Add(-7*24*time.Hour).Unix(), repository.deleteCalls[0])
	assert.Equal(t, base.Add(12*time.Hour-7*24*time.Hour).Unix(), repository.deleteCalls[1])
}

func TestNextProbeSlotAlwaysAdvancesWithoutBackfill(t *testing.T) {
	assert.Equal(t, time.Date(2026, 8, 16, 12, 1, 0, 0, time.UTC), nextProbeSlot(time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC), time.Minute))
	assert.Equal(t, time.Date(2026, 8, 16, 12, 2, 0, 0, time.UTC), nextProbeSlot(time.Date(2026, 8, 16, 12, 1, 59, 0, time.UTC), time.Minute))
}

func TestProbeSlotAfterWakeSkipsMissedMinutes(t *testing.T) {
	planned := time.Date(2026, 8, 17, 10, 1, 0, 0, time.UTC)
	woke := time.Date(2026, 8, 17, 10, 3, 27, 0, time.UTC)

	assert.Equal(t, time.Date(2026, 8, 17, 10, 3, 0, 0, time.UTC), probeSlotAfterWake(planned, woke, time.Minute))
}

func TestProbeSlotAfterWakeKeepsOnTimeSlot(t *testing.T) {
	planned := time.Date(2026, 8, 17, 10, 1, 0, 0, time.UTC)

	assert.Equal(t, planned, probeSlotAfterWake(planned, planned, time.Minute))
}

func TestSchedulerDerivesProductionRenewIntervalFromEachSlotLeaseDuration(t *testing.T) {
	scheduler := &Scheduler{}
	require.Zero(t, scheduler.renewInterval)

	shortLease := 20 * time.Second
	longLease := 75 * time.Second
	assert.Equal(t, shortLease/3, scheduler.renewIntervalFor(shortLease))
	assert.Equal(t, 20*time.Second, scheduler.renewIntervalFor(longLease))
}

func TestSchedulerRunCancellationDoesNotDrainExpiredTimer(t *testing.T) {
	setting := schedulerSetting(0, 1)
	scheduler := newTestScheduler(t, setting, targetLoaderFunc(func(_ context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
		return loadedSchedulerTarget(target), nil
	}), newFakeProbeRepository())
	timerCreated := make(chan struct{})
	scheduler.newTimer = func(time.Duration) schedulerTimer {
		close(timerCreated)
		return schedulerTimer{
			channel: make(chan time.Time),
			stop:    func() bool { return false },
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()

	select {
	case <-timerCreated:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not create timer within one second")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not exit after timer-boundary cancellation")
	}
}
