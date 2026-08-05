package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func prepareGroupProbeResults(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&GroupProbeResult{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&GroupProbeResult{}).Error)
}

func TestGroupProbeResultMigrationAndPersistence(t *testing.T) {
	prepareGroupProbeResults(t)
	channelID := 42
	result := &GroupProbeResult{
		TaskID:      "systask_test",
		GroupName:   "codex",
		DisplayName: "Codex",
		ModelName:   "gpt-5.5",
		ChannelID:   &channelID,
		Success:     false,
		LatencyMS:   321,
		ErrorCode:   "UPSTREAM_FAILURE",
		ErrorMessage: "Authorization: Bearer top-secret; " +
			"url=https://provider.example/v1?api_key=secret-key; response body: private payload",
		CheckedAt: 1_700_000_000,
	}
	require.NoError(t, CreateGroupProbeResult(result))
	require.NotZero(t, result.ID)

	var got GroupProbeResult
	require.NoError(t, DB.First(&got, result.ID).Error)
	require.NotNil(t, got.ChannelID)
	assert.Equal(t, channelID, *got.ChannelID)
	assert.Equal(t, "upstream_failure", got.ErrorCode)
	assert.NotContains(t, got.ErrorMessage, "top-secret")
	assert.NotContains(t, got.ErrorMessage, "secret-key")
	assert.NotContains(t, got.ErrorMessage, "private payload")
	assert.LessOrEqual(t, len([]rune(got.ErrorMessage)), MaxGroupProbeErrorMessageLength)

	assert.True(t, DB.Migrator().HasIndex(&GroupProbeResult{}, GroupProbeGroupCheckedIndex))
	assert.True(t, DB.Migrator().HasIndex(&GroupProbeResult{}, GroupProbeCheckedAtIndex))
}

func TestSanitizeGroupProbeError(t *testing.T) {
	input := "Authorization=Bearer abc.def; api_key='key-value'; token=token-value; " +
		"https://user:password@example.com/v1?secret=query-value&safe=no; upstream body: " + strings.Repeat("x", 2_000)
	got := SanitizeGroupProbeError(input)
	for _, secret := range []string{"abc.def", "key-value", "token-value", "password", "query-value", strings.Repeat("x", 20)} {
		assert.NotContains(t, got, secret)
	}
	assert.LessOrEqual(t, len([]rune(got)), MaxGroupProbeErrorMessageLength)

	assert.Equal(t, "probe_failed", SanitizeGroupProbeErrorCode(" !!! "))
	assert.Equal(t, "timeout_error", SanitizeGroupProbeErrorCode(" Timeout Error "))
	assert.LessOrEqual(t, len(SanitizeGroupProbeErrorCode(strings.Repeat("a", 100))), MaxGroupProbeErrorCodeLength)
}

func TestDeleteExpiredGroupProbeResults(t *testing.T) {
	prepareGroupProbeResults(t)
	now := int64(1_700_000_000)
	cutoff := now - 7*24*60*60
	for _, checkedAt := range []int64{cutoff - 1, cutoff, now} {
		require.NoError(t, DB.Create(&GroupProbeResult{GroupName: "codex", DisplayName: "Codex", ModelName: "gpt-5.5", CheckedAt: checkedAt}).Error)
	}

	deleted, err := DeleteExpiredGroupProbeResults(7, now)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	var rows []GroupProbeResult
	require.NoError(t, DB.Order("checked_at asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, cutoff, rows[0].CheckedAt)

	_, err = DeleteExpiredGroupProbeResults(0, now)
	assert.Error(t, err)
}

func TestAggregateGroupProbeResultsBuildsRollingWindow(t *testing.T) {
	now := int64(1_700_001_400)
	target := GroupProbeTarget{GroupName: "codex", DisplayName: "Codex", ModelName: "gpt-5.5", IntervalMinutes: 10, TimeoutSeconds: 45}
	bucketSeconds := int64(target.IntervalMinutes * 60)
	currentBucket := now - now%bucketSeconds
	rows := []GroupProbeResult{
		{GroupName: "codex", Success: true, LatencyMS: 100, CheckedAt: now - 120},
		{GroupName: "codex", Success: false, LatencyMS: 900, CheckedAt: now - 240},
		{GroupName: "codex", Success: true, LatencyMS: 300, CheckedAt: now - 360},
		{GroupName: "codex", Success: true, LatencyMS: 500, CheckedAt: currentBucket - bucketSeconds + 30},
		{GroupName: "other", Success: false, CheckedAt: now - 10},
		{GroupName: "codex", Success: false, CheckedAt: now - GroupProbeWindowSeconds - 1},
	}

	got := AggregateGroupProbeResults(target, rows, now)
	assert.Equal(t, GroupProbeStateHealthy, got.State)
	assert.False(t, got.Stale)
	require.NotNil(t, got.LatestCheckedAt)
	assert.Equal(t, now-120, *got.LatestCheckedAt)
	assert.EqualValues(t, 4, got.SampleCount)
	require.NotNil(t, got.Availability)
	assert.InDelta(t, 0.75, *got.Availability, 0.0001)
	require.NotNil(t, got.AverageLatencyMS)
	assert.EqualValues(t, 300, *got.AverageLatencyMS)
	require.Len(t, got.Buckets, 144)
	assert.Equal(t, currentBucket-int64(len(got.Buckets)-1)*bucketSeconds, got.Buckets[0].StartedAt)
	assert.Equal(t, currentBucket, got.Buckets[len(got.Buckets)-1].StartedAt)
	assert.Equal(t, GroupProbeStateHealthy, got.Buckets[len(got.Buckets)-2].State)
	assert.Equal(t, GroupProbeStateDegraded, got.Buckets[len(got.Buckets)-1].State)
}

func TestAggregateGroupProbeResultsUsesEffectiveIntervalForDynamicBuckets(t *testing.T) {
	now := int64(1_700_001_400)
	tests := []struct {
		name            string
		intervalMinutes int
		wantEffective   int
		wantBucketCount int
	}{
		{name: "five minutes reaches maximum", intervalMinutes: 5, wantEffective: 5, wantBucketCount: 288},
		{name: "ten minutes", intervalMinutes: 10, wantEffective: 10, wantBucketCount: 144},
		{name: "thirty minutes", intervalMinutes: 30, wantEffective: 30, wantBucketCount: 48},
		{name: "sixty minutes", intervalMinutes: 60, wantEffective: 60, wantBucketCount: 24},
		{name: "non divisor rounds up", intervalMinutes: 7, wantEffective: 7, wantBucketCount: 206},
		{name: "zero uses default", intervalMinutes: 0, wantEffective: GroupProbeDefaultIntervalMinutes, wantBucketCount: 144},
		{name: "below minimum uses default", intervalMinutes: 4, wantEffective: GroupProbeDefaultIntervalMinutes, wantBucketCount: 144},
		{name: "above maximum uses default", intervalMinutes: 1441, wantEffective: GroupProbeDefaultIntervalMinutes, wantBucketCount: 144},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := GroupProbeTarget{GroupName: "codex", IntervalMinutes: tt.intervalMinutes}
			got := AggregateGroupProbeResults(target, nil, now)

			assert.Equal(t, tt.wantEffective, got.IntervalMinutes)
			require.Len(t, got.Buckets, tt.wantBucketCount)
			assert.LessOrEqual(t, len(got.Buckets), GroupProbeMaxBucketCount)

			bucketSeconds := int64(tt.wantEffective * 60)
			currentBucket := now - now%bucketSeconds
			assert.Equal(t, currentBucket, got.Buckets[len(got.Buckets)-1].StartedAt)
			for i := 1; i < len(got.Buckets); i++ {
				assert.Equal(t, bucketSeconds, got.Buckets[i].StartedAt-got.Buckets[i-1].StartedAt)
			}
		})
	}
}

func TestAggregateGroupProbeResultsAvailabilityUsesExactTwentyFourHourWindow(t *testing.T) {
	now := int64(1_700_001_400)
	target := GroupProbeTarget{GroupName: "codex", IntervalMinutes: 10, TimeoutSeconds: 45}
	rows := []GroupProbeResult{
		{GroupName: "codex", Success: false, LatencyMS: 800, CheckedAt: now - GroupProbeWindowSeconds},
		{GroupName: "codex", Success: true, LatencyMS: 100, CheckedAt: now - GroupProbeWindowSeconds - 1},
		{GroupName: "codex", Success: true, LatencyMS: 200, CheckedAt: now + 1},
	}

	got := AggregateGroupProbeResults(target, rows, now)
	require.NotNil(t, got.Availability)
	assert.Equal(t, 1, got.SampleCount)
	assert.InDelta(t, 0, *got.Availability, 0.0001)
	assert.Nil(t, got.AverageLatencyMS)
}

func TestAggregateGroupProbeResultsCurrentStateBoundaries(t *testing.T) {
	now := int64(1_700_001_000)
	target := GroupProbeTarget{GroupName: "codex", DisplayName: "Codex", ModelName: "gpt-5.5", IntervalMinutes: 10, TimeoutSeconds: 45}
	rows := func(values ...bool) []GroupProbeResult {
		result := make([]GroupProbeResult, 0, len(values))
		for i, success := range values {
			result = append(result, GroupProbeResult{GroupName: "codex", Success: success, CheckedAt: now - int64(i+1)*10})
		}
		return result
	}

	assert.Equal(t, GroupProbeStateHealthy, AggregateGroupProbeResults(target, rows(true), now).State)
	assert.Equal(t, GroupProbeStateHealthy, AggregateGroupProbeResults(target, rows(true, true, false), now).State)
	assert.Equal(t, GroupProbeStateDegraded, AggregateGroupProbeResults(target, rows(true, false, false), now).State)
	assert.Equal(t, GroupProbeStateUnknown, AggregateGroupProbeResults(target, rows(false, true), now).State)
	assert.Equal(t, GroupProbeStateDown, AggregateGroupProbeResults(target, rows(false, false, false), now).State)
	assert.Equal(t, GroupProbeStateUnknown, AggregateGroupProbeResults(target, nil, now).State)

	boundary := int64(2*target.IntervalMinutes*60 + target.TimeoutSeconds)
	fresh := []GroupProbeResult{{GroupName: "codex", Success: true, CheckedAt: now - boundary}}
	assert.False(t, AggregateGroupProbeResults(target, fresh, now).Stale)
	fresh[0].CheckedAt--
	stale := AggregateGroupProbeResults(target, fresh, now)
	assert.True(t, stale.Stale)
	assert.Equal(t, GroupProbeStateUnknown, stale.State)
}

func TestGroupProbePublicDTOIsAnExplicitAllowlist(t *testing.T) {
	latest := int64(1_700_000_000)
	availability := 1.0
	latency := int64(120)
	dto := GroupProbePublicStatus{
		GroupName:        "codex",
		DisplayName:      "Codex",
		ModelName:        "gpt-5.5",
		State:            GroupProbeStateHealthy,
		Availability:     &availability,
		AverageLatencyMS: &latency,
		SampleCount:      3,
		LatestCheckedAt:  &latest,
		Stale:            false,
		IntervalMinutes:  10,
		Buckets:          []GroupProbeBucket{},
	}
	raw, err := common.Marshal(dto)
	require.NoError(t, err)
	jsonText := string(raw)
	for _, forbidden := range []string{"channel_id", "task_id", "error_code", "error_message", "authorization", "credential"} {
		assert.NotContains(t, jsonText, forbidden)
	}
	for _, allowed := range []string{"group_name", "display_name", "model_name", "state", "availability", "average_latency_ms", "sample_count", "latest_checked_at", "stale", "interval_minutes", "buckets"} {
		assert.Contains(t, jsonText, allowed)
	}
}
