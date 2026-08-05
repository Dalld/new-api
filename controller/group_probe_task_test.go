package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/group_probe_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestGroupProbeRunner(results *[]model.GroupProbeResult) groupProbeTaskRunner {
	now := time.Unix(1_700_000_000, 0)
	return groupProbeTaskRunner{
		now: func() time.Time { return now },
		hasEnabledAbility: func(_, _ string) (bool, error) {
			return true, nil
		},
		selectChannel: func(_ context.Context, _, _, _ string, _ int) (*model.Channel, error) {
			return &model.Channel{Id: 7}, nil
		},
		resolveTestUserID: func() (int, error) { return 1, nil },
		testChannel: func(_ context.Context, _ *model.Channel, _ int, _, _ string, _ bool, _ channelTestOptions) testResult {
			return testResult{}
		},
		createResult: func(result *model.GroupProbeResult) error {
			*results = append(*results, *result)
			return nil
		},
		deleteExpired: func(_ int, _ int64) (int64, error) { return 0, nil },
	}
}

func testGroupProbeSetting(groups ...group_probe_setting.GroupMapping) group_probe_setting.Setting {
	return group_probe_setting.Setting{
		Enabled:         true,
		IntervalMinutes: 10,
		RetentionDays:   7,
		TimeoutSeconds:  45,
		Groups:          groups,
	}
}

func TestGroupProbeTaskUsesGroupRetryZeroAndPersistsEveryMapping(t *testing.T) {
	var results []model.GroupProbeResult
	runner := newTestGroupProbeRunner(&results)
	var selectedGroups []string
	var selectedRetries []int
	var usingGroups []string
	var endpoints []string
	var cleanedRetention int
	runner.selectChannel = func(_ context.Context, group, _ string, requestPath string, retry int) (*model.Channel, error) {
		selectedGroups = append(selectedGroups, group)
		selectedRetries = append(selectedRetries, retry)
		assert.Equal(t, "/v1/chat/completions", requestPath)
		return &model.Channel{Id: len(selectedGroups), Type: constant.ChannelTypeCodex}, nil
	}
	runner.testChannel = func(_ context.Context, _ *model.Channel, _ int, _ string, endpoint string, _ bool, options channelTestOptions) testResult {
		usingGroups = append(usingGroups, options.UsingGroup)
		endpoints = append(endpoints, endpoint)
		assert.False(t, options.RecordConsumeLog)
		assert.False(t, options.LogDetails)
		return testResult{}
	}
	runner.deleteExpired = func(retentionDays int, now int64) (int64, error) {
		cleanedRetention = retentionDays
		assert.EqualValues(t, 1_700_000_000, now)
		return 3, nil
	}

	setting := testGroupProbeSetting(
		group_probe_setting.GroupMapping{Group: "codex", DisplayName: "Codex", Model: "gpt-5.5"},
		group_probe_setting.GroupMapping{Group: "provip", DisplayName: "Pro VIP", Model: "gpt-5.5"},
	)
	summary, err := runGroupProbeTask(context.Background(), "systask_probe", setting, runner)
	require.NoError(t, err)
	assert.Equal(t, groupProbeTaskSummary{Total: 2, Succeeded: 2}, summary)
	assert.Equal(t, []string{"codex", "provip"}, selectedGroups)
	assert.Equal(t, []int{0, 0}, selectedRetries)
	assert.Equal(t, selectedGroups, usingGroups)
	assert.Equal(t, []string{string(constant.EndpointTypeOpenAIResponse), string(constant.EndpointTypeOpenAIResponse)}, endpoints)
	assert.Equal(t, 7, cleanedRetention)
	require.Len(t, results, 2)
	for index, result := range results {
		assert.Equal(t, "systask_probe", result.TaskID)
		assert.Equal(t, setting.Groups[index].Group, result.GroupName)
		assert.True(t, result.Success)
	}
}

func TestGroupProbeTaskContinuesAfterMappingFailures(t *testing.T) {
	var results []model.GroupProbeResult
	runner := newTestGroupProbeRunner(&results)
	runner.hasEnabledAbility = func(group, _ string) (bool, error) {
		return group != "missing", nil
	}
	runner.selectChannel = func(_ context.Context, group, _, _ string, _ int) (*model.Channel, error) {
		if group == "no-channel" {
			return nil, nil
		}
		return &model.Channel{Id: 7}, nil
	}
	runner.testChannel = func(_ context.Context, _ *model.Channel, _ int, modelName, _ string, _ bool, _ channelTestOptions) testResult {
		if modelName == "provider-error" {
			return testResult{newAPIError: types.NewError(
				errors.New("Authorization: Bearer secret response body: private"),
				types.ErrorCodeChannelInvalidKey,
			)}
		}
		return testResult{}
	}

	setting := testGroupProbeSetting(
		group_probe_setting.GroupMapping{Group: "missing", DisplayName: "Missing", Model: "gpt-5.5"},
		group_probe_setting.GroupMapping{Group: "no-channel", DisplayName: "No Channel", Model: "gpt-5.5"},
		group_probe_setting.GroupMapping{Group: "failing", DisplayName: "Failing", Model: "provider-error"},
		group_probe_setting.GroupMapping{Group: "healthy", DisplayName: "Healthy", Model: "gpt-5.5"},
	)
	summary, err := runGroupProbeTask(context.Background(), "systask_probe", setting, runner)
	require.NoError(t, err)
	assert.Equal(t, groupProbeTaskSummary{Total: 4, Succeeded: 1, Failed: 3}, summary)
	require.Len(t, results, 4)
	assert.Equal(t, "no_enabled_ability", results[0].ErrorCode)
	assert.Equal(t, "no_available_channel", results[1].ErrorCode)
	assert.Equal(t, "channel_invalid_key", results[2].ErrorCode)
	assert.NotContains(t, results[2].ErrorMessage, "secret")
	assert.NotContains(t, results[2].ErrorMessage, "private")
	assert.True(t, results[3].Success)
}

func TestGroupProbeTaskRecordsTimeoutAndContinues(t *testing.T) {
	var results []model.GroupProbeResult
	runner := newTestGroupProbeRunner(&results)
	runner.testChannel = func(_ context.Context, _ *model.Channel, _ int, modelName, _ string, _ bool, _ channelTestOptions) testResult {
		if modelName == "slow" {
			return testResult{localErr: context.DeadlineExceeded}
		}
		return testResult{}
	}

	setting := testGroupProbeSetting(
		group_probe_setting.GroupMapping{Group: "slow", DisplayName: "Slow", Model: "slow"},
		group_probe_setting.GroupMapping{Group: "healthy", DisplayName: "Healthy", Model: "gpt-5.5"},
	)
	summary, err := runGroupProbeTask(context.Background(), "systask_probe", setting, runner)
	require.NoError(t, err)
	assert.Equal(t, groupProbeTaskSummary{Total: 2, Succeeded: 1, Failed: 1}, summary)
	require.Len(t, results, 2)
	assert.Equal(t, "timeout", results[0].ErrorCode)
	assert.True(t, results[1].Success)
}

func TestGroupProbeTaskCancellationStopsRemainingWorkAndCleanup(t *testing.T) {
	var results []model.GroupProbeResult
	runner := newTestGroupProbeRunner(&results)
	ctx, cancel := context.WithCancel(context.Background())
	probes := 0
	cleanups := 0
	runner.testChannel = func(_ context.Context, _ *model.Channel, _ int, _ string, _ string, _ bool, _ channelTestOptions) testResult {
		probes++
		cancel()
		return testResult{}
	}
	runner.deleteExpired = func(_ int, _ int64) (int64, error) {
		cleanups++
		return 0, nil
	}

	setting := testGroupProbeSetting(
		group_probe_setting.GroupMapping{Group: "first", DisplayName: "First", Model: "gpt-5.5"},
		group_probe_setting.GroupMapping{Group: "second", DisplayName: "Second", Model: "gpt-5.5"},
	)
	summary, err := runGroupProbeTask(ctx, "systask_probe", setting, runner)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, groupProbeTaskSummary{Total: 2}, summary)
	assert.Equal(t, 1, probes)
	assert.Zero(t, cleanups)
	assert.Empty(t, results)
}

func TestGroupProbeHandlerScheduledContract(t *testing.T) {
	original := group_probe_setting.GetSetting()
	t.Cleanup(func() { require.NoError(t, group_probe_setting.SetSetting(original, nil)) })
	configured := group_probe_setting.DefaultSetting()
	configured.Enabled = true
	configured.IntervalMinutes = 17
	require.NoError(t, group_probe_setting.SetSetting(configured, nil))

	handler := groupProbeHandler{}
	assert.Equal(t, model.SystemTaskTypeGroupProbe, handler.Type())
	assert.True(t, handler.Enabled())
	assert.Equal(t, 17*time.Minute, handler.Interval())
	assert.Nil(t, handler.NewPayload())
}
