package public_status_probe

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/google/uuid"
)

const (
	retentionInterval = 12 * time.Hour
	leaseSafetyMargin = 15 * time.Second
)

type probeRepository interface {
	ResultExists(targetKey string, slotStartedAt int64) (bool, error)
	AcquireLease(targetKey, ownerID string, now, leaseUntil int64) (bool, error)
	RenewLease(targetKey, ownerID string, now, leaseUntil int64) (bool, error)
	CompleteLease(context.Context, string, string, int64, int64) (bool, error)
	CreateResult(context.Context, *model.PublicStatusProbeResult, string, int64) (bool, error)
	DeleteBefore(cutoff int64) (int64, error)
}

type modelProbeRepository struct{}

func (modelProbeRepository) ResultExists(targetKey string, slotStartedAt int64) (bool, error) {
	return model.PublicStatusProbeResultExists(targetKey, slotStartedAt)
}

func (modelProbeRepository) AcquireLease(targetKey, ownerID string, now, leaseUntil int64) (bool, error) {
	return model.AcquirePublicStatusProbeLease(targetKey, ownerID, now, leaseUntil)
}

func (modelProbeRepository) RenewLease(targetKey, ownerID string, now, leaseUntil int64) (bool, error) {
	return model.RenewPublicStatusProbeLease(targetKey, ownerID, now, leaseUntil)
}

func (modelProbeRepository) CompleteLease(ctx context.Context, targetKey, ownerID string, now, holdUntil int64) (bool, error) {
	return model.CompletePublicStatusProbeLeaseWithContext(ctx, targetKey, ownerID, now, holdUntil)
}

func (modelProbeRepository) CreateResult(ctx context.Context, result *model.PublicStatusProbeResult, ownerID string, now int64) (bool, error) {
	return model.CreatePublicStatusProbeResultIfLeaseOwner(ctx, result, ownerID, now)
}

func (modelProbeRepository) DeleteBefore(cutoff int64) (int64, error) {
	return model.DeletePublicStatusProbeResultsBefore(cutoff)
}

type SettingProvider interface {
	CurrentSetting() publicstatusprobesetting.Setting
}

type RuntimeSettingProvider struct{}

func (RuntimeSettingProvider) CurrentSetting() publicstatusprobesetting.Setting {
	return publicstatusprobesetting.CurrentSetting()
}

type Scheduler struct {
	provider SettingProvider
	loader   TargetLoader
	repo     probeRepository
	client   *http.Client
	ownerID  string
	now      func() time.Time

	adapterFor      func(Snapshot, *http.Client) (Adapter, error)
	ping            func(context.Context, *http.Client, string, time.Duration) PingResult
	runConversation func(context.Context, Adapter, Snapshot, time.Duration, time.Duration) ConversationResult

	running       atomic.Bool
	lastRetention atomic.Int64
	renewInterval time.Duration
}

func NewScheduler(provider SettingProvider, loader TargetLoader, repository probeRepository, client *http.Client) (*Scheduler, error) {
	if provider == nil {
		return nil, codedError(ErrorInvalidTarget)
	}
	setting := provider.CurrentSetting()
	if setting.Interval != time.Minute || setting.Concurrency < 1 || setting.Concurrency > 20 || setting.RetentionDays < 1 || setting.RetentionDays > 30 {
		return nil, codedError(ErrorInvalidTarget)
	}
	if loader == nil || repository == nil {
		return nil, codedError(ErrorInvalidTarget)
	}
	return &Scheduler{
		provider:        provider,
		loader:          loader,
		repo:            repository,
		client:          client,
		ownerID:         uuid.NewString(),
		now:             time.Now,
		adapterFor:      AdapterFor,
		ping:            Ping,
		runConversation: RunConversation,
	}, nil
}

func Start(ctx context.Context, provider SettingProvider) <-chan struct{} {
	done := make(chan struct{})
	scheduler, err := NewScheduler(provider, NewDBTargetLoader(model.DB), modelProbeRepository{}, NewHTTPClient())
	if err != nil {
		common.SysLog("public status probe scheduler disabled: invalid configuration")
		close(done)
		return done
	}
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()
	return done
}

func (scheduler *Scheduler) Run(ctx context.Context) {
	if scheduler == nil || ctx == nil {
		return
	}
	for {
		now := scheduler.now().UTC()
		next := nextProbeSlot(now, time.Minute)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
			woke := scheduler.now().UTC()
			scheduler.runSlot(ctx, probeSlotAfterWake(next, woke, time.Minute))
		}
	}
}

func nextProbeSlot(now time.Time, interval time.Duration) time.Time {
	return now.UTC().Truncate(interval).Add(interval)
}

func probeSlotAfterWake(planned, woke time.Time, interval time.Duration) time.Time {
	planned = planned.UTC().Truncate(interval)
	current := woke.UTC().Truncate(interval)
	if current.After(planned) {
		return current
	}
	return planned
}

func (scheduler *Scheduler) runSlot(ctx context.Context, slot time.Time) bool {
	if scheduler == nil || !scheduler.running.CompareAndSwap(false, true) {
		return false
	}
	defer scheduler.running.Store(false)

	setting := cloneSetting(scheduler.provider.CurrentSetting())
	slot = slot.UTC().Truncate(time.Minute)
	if !setting.Enabled || len(setting.Targets) == 0 {
		if ctx.Err() == nil {
			scheduler.runRetention(slot, setting.RetentionDays)
		}
		return true
	}

	semaphore := make(chan struct{}, setting.Concurrency)
	var waitGroup sync.WaitGroup
	for _, configuredTarget := range setting.Targets {
		target := configuredTarget
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			scheduler.probeTarget(ctx, target, slot, setting)
		}()
	}
	waitGroup.Wait()
	if ctx.Err() == nil {
		scheduler.runRetention(slot, setting.RetentionDays)
	}
	return true
}

func (scheduler *Scheduler) probeTarget(ctx context.Context, target publicstatusprobesetting.Target, slot time.Time, setting publicstatusprobesetting.Setting) {
	slotUnix := slot.Unix()
	exists, err := scheduler.repo.ResultExists(target.Key, slotUnix)
	if err != nil || exists {
		return
	}

	now := scheduler.now().UTC()
	leaseDuration := maxDuration(setting.PingTimeout, setting.ChatTimeout) + leaseSafetyMargin
	acquired, err := scheduler.repo.AcquireLease(target.Key, scheduler.ownerID, now.Unix(), now.Add(leaseDuration).Unix())
	if err != nil || !acquired {
		return
	}
	probeContext, cancelProbe := context.WithCancel(ctx)
	renewalDone := make(chan struct{})
	renewInterval := scheduler.renewIntervalFor(leaseDuration)
	go scheduler.renewLease(probeContext, cancelProbe, renewalDone, target.Key, leaseDuration, renewInterval)
	defer func() {
		cancelProbe()
		<-renewalDone
		scheduler.completeLease(target.Key)
	}()

	exists, err = scheduler.repo.ResultExists(target.Key, slotUnix)
	if err != nil || exists {
		return
	}

	loaded, loadErr := scheduler.loader.Load(probeContext, target)
	if probeContext.Err() != nil {
		return
	}
	if loadErr != nil {
		scheduler.storeResult(probeContext, target, slotUnix, ProbeResult{
			State:     StateFailed,
			ErrorCode: ErrorCodeOf(loadErr),
		})
		return
	}
	adapter, adapterErr := scheduler.adapterFor(loaded.Snapshot, scheduler.client)
	if adapterErr != nil {
		scheduler.storeLoadedResult(probeContext, loaded, slotUnix, ProbeResult{
			State:     StateFailed,
			ErrorCode: ErrorCodeOf(adapterErr),
		})
		return
	}

	pingResult := make(chan PingResult, 1)
	conversationResult := make(chan ConversationResult, 1)
	go func() {
		pingResult <- scheduler.ping(probeContext, scheduler.client, loaded.Snapshot.BaseURL, setting.PingTimeout)
	}()
	go func() {
		conversationResult <- scheduler.runConversation(probeContext, adapter, loaded.Snapshot, setting.ChatTimeout, setting.DegradedLatency)
	}()

	combined := CombineResults(<-pingResult, <-conversationResult)
	if probeContext.Err() != nil {
		return
	}
	scheduler.storeLoadedResult(probeContext, loaded, slotUnix, combined)
}

func (scheduler *Scheduler) renewLease(ctx context.Context, cancel context.CancelFunc, done chan<- struct{}, targetKey string, leaseDuration, renewInterval time.Duration) {
	defer close(done)
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := scheduler.now().UTC()
			renewed, err := scheduler.repo.RenewLease(targetKey, scheduler.ownerID, now.Unix(), now.Add(leaseDuration).Unix())
			if err != nil || !renewed {
				cancel()
				return
			}
		}
	}
}

func (scheduler *Scheduler) renewIntervalFor(leaseDuration time.Duration) time.Duration {
	if scheduler.renewInterval > 0 {
		return scheduler.renewInterval
	}
	return minDuration(leaseDuration/3, 20*time.Second)
}

func (scheduler *Scheduler) completeLease(targetKey string) {
	now := scheduler.now().UTC()
	holdUntil := nextProbeSlot(now, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = scheduler.repo.CompleteLease(ctx, targetKey, scheduler.ownerID, now.Unix(), holdUntil.Unix())
}

func (scheduler *Scheduler) storeResult(ctx context.Context, target publicstatusprobesetting.Target, slotStartedAt int64, result ProbeResult) {
	errorCode := ""
	if result.ErrorCode != "" {
		errorCode = string(stableErrorCode(result.ErrorCode))
	}
	scheduler.store(ctx, &model.PublicStatusProbeResult{
		TargetKey:     target.Key,
		GroupName:     target.Group,
		DisplayName:   target.DisplayName,
		ModelName:     target.Model,
		ChannelID:     target.ChannelID,
		SlotStartedAt: slotStartedAt,
		CheckedAt:     scheduler.now().Unix(),
		State:         model.PublicStatusProbeState(result.State),
		PingLatencyMS: result.PingLatencyMS,
		ChatLatencyMS: result.ChatLatencyMS,
		ErrorCode:     errorCode,
	})
}

func (scheduler *Scheduler) storeLoadedResult(ctx context.Context, target LoadedTarget, slotStartedAt int64, result ProbeResult) {
	errorCode := ""
	if result.ErrorCode != "" {
		errorCode = string(stableErrorCode(result.ErrorCode))
	}
	scheduler.store(ctx, &model.PublicStatusProbeResult{
		TargetKey:     target.TargetKey,
		GroupName:     target.Group,
		DisplayName:   target.DisplayName,
		ModelName:     target.ModelName,
		ChannelID:     target.ChannelID,
		SlotStartedAt: slotStartedAt,
		CheckedAt:     scheduler.now().Unix(),
		State:         model.PublicStatusProbeState(result.State),
		PingLatencyMS: result.PingLatencyMS,
		ChatLatencyMS: result.ChatLatencyMS,
		ErrorCode:     errorCode,
	})
}

func (scheduler *Scheduler) store(ctx context.Context, result *model.PublicStatusProbeResult) {
	if result.State == "" {
		result.State = model.PublicStatusProbeStateFailed
	}
	_, _ = scheduler.repo.CreateResult(ctx, result, scheduler.ownerID, scheduler.now().Unix())
}

func (scheduler *Scheduler) runRetention(now time.Time, retentionDays int) {
	nowUnix := now.Unix()
	last := scheduler.lastRetention.Load()
	if last != 0 && nowUnix-last < int64(retentionInterval/time.Second) {
		return
	}
	if !scheduler.lastRetention.CompareAndSwap(last, nowUnix) {
		return
	}
	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if _, err := scheduler.repo.DeleteBefore(cutoff); err != nil {
		common.SysLog(fmt.Sprintf("public status probe retention failed at %d", nowUnix))
	}
}

func cloneSetting(setting publicstatusprobesetting.Setting) publicstatusprobesetting.Setting {
	setting.Targets = append([]publicstatusprobesetting.Target(nil), setting.Targets...)
	return setting
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
