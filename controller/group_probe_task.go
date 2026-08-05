package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/group_probe_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type groupProbeHandler struct{}

func (groupProbeHandler) Type() string { return model.SystemTaskTypeGroupProbe }

func (groupProbeHandler) Enabled() bool {
	return group_probe_setting.GetSetting().Enabled
}

func (groupProbeHandler) Interval() time.Duration {
	minutes := group_probe_setting.GetSetting().IntervalMinutes
	if minutes <= 0 {
		minutes = group_probe_setting.DefaultSetting().IntervalMinutes
	}
	return time.Duration(minutes) * time.Minute
}

func (groupProbeHandler) NewPayload() any { return nil }

func (groupProbeHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	setting := group_probe_setting.GetSetting()
	summary, err := runGroupProbeTask(ctx, task.TaskID, setting, defaultGroupProbeTaskRunner())
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

type groupProbeTaskSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type groupProbeTaskRunner struct {
	now               func() time.Time
	hasEnabledAbility func(group, modelName string) (bool, error)
	selectChannel     func(ctx context.Context, group, modelName, requestPath string, retry int) (*model.Channel, error)
	resolveTestUserID func() (int, error)
	testChannel       func(ctx context.Context, channel *model.Channel, testUserID int, modelName, endpointType string, isStream bool, options channelTestOptions) testResult
	createResult      func(result *model.GroupProbeResult) error
	deleteExpired     func(retentionDays int, now int64) (int64, error)
}

func defaultGroupProbeTaskRunner() groupProbeTaskRunner {
	return groupProbeTaskRunner{
		now: time.Now,
		hasEnabledAbility: func(group, modelName string) (bool, error) {
			var count int64
			err := model.DB.Model(&model.Ability{}).
				Where(&model.Ability{Group: group, Model: modelName, Enabled: true}).
				Count(&count).Error
			return count > 0, err
		},
		selectChannel: func(ctx context.Context, group, modelName, requestPath string, retry int) (*model.Channel, error) {
			ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			ginContext.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, requestPath, nil)
			channel, _, err := service.CacheGetRandomSatisfiedChannel(&service.RetryParam{
				Ctx:         ginContext,
				TokenGroup:  group,
				ModelName:   modelName,
				RequestPath: requestPath,
				Retry:       &retry,
			})
			return channel, err
		},
		resolveTestUserID: func() (int, error) {
			return resolveChannelTestUserID(nil)
		},
		testChannel: func(ctx context.Context, channel *model.Channel, testUserID int, modelName, endpointType string, isStream bool, options channelTestOptions) testResult {
			return testChannelWithOptions(ctx, channel, testUserID, modelName, endpointType, isStream, options)
		},
		createResult:  model.CreateGroupProbeResult,
		deleteExpired: model.DeleteExpiredGroupProbeResults,
	}
}

func runGroupProbeTask(ctx context.Context, taskID string, setting group_probe_setting.Setting, runner groupProbeTaskRunner) (groupProbeTaskSummary, error) {
	summary := groupProbeTaskSummary{Total: len(setting.Groups)}
	if ctx == nil {
		ctx = context.Background()
	}
	timeoutSeconds := setting.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = group_probe_setting.DefaultSetting().TimeoutSeconds
	}

	for _, mapping := range setting.Groups {
		if err := ctx.Err(); err != nil {
			return summary, err
		}

		result := runSingleGroupProbe(ctx, taskID, mapping, time.Duration(timeoutSeconds)*time.Second, runner)
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if err := runner.createResult(result); err != nil {
			return summary, fmt.Errorf("persist group probe result: %w", err)
		}
		if result.Success {
			summary.Succeeded++
		} else {
			summary.Failed++
		}
	}

	if err := ctx.Err(); err != nil {
		return summary, err
	}
	if _, err := runner.deleteExpired(setting.RetentionDays, runner.now().Unix()); err != nil {
		return summary, fmt.Errorf("delete expired group probe results: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	return summary, nil
}

func runSingleGroupProbe(ctx context.Context, taskID string, mapping group_probe_setting.GroupMapping, timeout time.Duration, runner groupProbeTaskRunner) (result *model.GroupProbeResult) {
	startedAt := runner.now()
	result = &model.GroupProbeResult{
		TaskID:      taskID,
		GroupName:   mapping.Group,
		DisplayName: mapping.DisplayName,
		ModelName:   mapping.Model,
		CheckedAt:   startedAt.Unix(),
	}
	defer func() {
		recovered := recover()
		result.LatencyMS = max(runner.now().Sub(startedAt).Milliseconds(), 0)
		result.CheckedAt = runner.now().Unix()
		if recovered != nil {
			setGroupProbeFailure(result, "probe_panic", fmt.Errorf("group probe panic: %v", recovered))
		}
	}()

	enabled, err := runner.hasEnabledAbility(mapping.Group, mapping.Model)
	if err != nil {
		setGroupProbeFailure(result, "ability_lookup_failed", err)
		return result
	}
	if !enabled {
		setGroupProbeFailure(result, "no_enabled_ability", errors.New("no enabled ability for configured group and model"))
		return result
	}

	requestPath := groupProbeRequestPath(mapping.Model)
	channel, err := runner.selectChannel(ctx, mapping.Group, mapping.Model, requestPath, 0)
	if err != nil {
		setGroupProbeFailure(result, "channel_selection_failed", err)
		return result
	}
	if channel == nil {
		setGroupProbeFailure(result, "no_available_channel", errors.New("no available channel for configured group and model"))
		return result
	}
	channelID := channel.Id
	result.ChannelID = &channelID

	testUserID, err := runner.resolveTestUserID()
	if err != nil {
		setGroupProbeFailure(result, "test_user_unavailable", err)
		return result
	}

	endpointType := normalizeChannelTestEndpoint(channel, mapping.Model, "")
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	testOutcome := runner.testChannel(
		probeCtx,
		channel,
		testUserID,
		mapping.Model,
		endpointType,
		shouldUseStreamForAutomaticChannelTest(channel),
		groupProbeChannelTestOptions(mapping.Group),
	)
	probeErr := probeCtx.Err()
	cancel()

	if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(testOutcome.localErr, context.DeadlineExceeded) || errors.Is(testOutcome.newAPIError, context.DeadlineExceeded) {
		setGroupProbeFailure(result, "timeout", context.DeadlineExceeded)
		return result
	}
	if testOutcome.newAPIError != nil {
		errorCode := string(testOutcome.newAPIError.GetErrorCode())
		if errorCode == "" {
			errorCode = "provider_failed"
		}
		setGroupProbeFailure(result, errorCode, testOutcome.newAPIError)
		return result
	}
	if testOutcome.localErr != nil {
		setGroupProbeFailure(result, "probe_failed", testOutcome.localErr)
		return result
	}

	result.Success = true
	return result
}

func groupProbeRequestPath(modelName string) string {
	modelName = strings.TrimSpace(modelName)
	lowerModel := strings.ToLower(modelName)
	switch {
	case strings.HasSuffix(modelName, ratio_setting.CompactModelSuffix):
		return "/v1/responses/compact"
	case strings.Contains(lowerModel, "codex"):
		return "/v1/responses"
	case strings.Contains(lowerModel, "rerank"):
		return "/v1/rerank"
	case strings.Contains(lowerModel, "embedding"), strings.HasPrefix(modelName, "m3e"), strings.Contains(modelName, "bge-"), strings.Contains(modelName, "embed"):
		return "/v1/embeddings"
	default:
		return "/v1/chat/completions"
	}
}

func setGroupProbeFailure(result *model.GroupProbeResult, errorCode string, err error) {
	result.Success = false
	result.ErrorCode = model.SanitizeGroupProbeErrorCode(errorCode)
	if err != nil {
		result.ErrorMessage = model.SanitizeGroupProbeError(err.Error())
	}
}

var _ service.ScheduledSystemTaskHandler = groupProbeHandler{}
