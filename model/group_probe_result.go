package model

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	GroupProbeGroupCheckedIndex = "idx_group_probe_group_checked"
	GroupProbeCheckedAtIndex    = "idx_group_probe_checked_at"

	MaxGroupProbeErrorCodeLength     = 64
	MaxGroupProbeErrorMessageLength  = 1024
	GroupProbeWindowMinutes          = 24 * 60
	GroupProbeWindowSeconds          = int64(GroupProbeWindowMinutes * 60)
	GroupProbeDefaultIntervalMinutes = 10
	GroupProbeMinIntervalMinutes     = 5
	GroupProbeMaxIntervalMinutes     = 24 * 60
	GroupProbeMaxBucketCount         = GroupProbeWindowMinutes / GroupProbeMinIntervalMinutes
)

type GroupProbeResult struct {
	ID           int64  `json:"id" gorm:"primaryKey"`
	TaskID       string `json:"task_id" gorm:"type:varchar(64)"`
	GroupName    string `json:"group_name" gorm:"type:varchar(64);index:idx_group_probe_group_checked,priority:1"`
	DisplayName  string `json:"display_name" gorm:"type:varchar(128)"`
	ModelName    string `json:"model_name" gorm:"type:varchar(255)"`
	ChannelID    *int   `json:"channel_id,omitempty"`
	Success      bool   `json:"success" gorm:"not null"`
	LatencyMS    int64  `json:"latency_ms" gorm:"bigint;not null;default:0"`
	ErrorCode    string `json:"error_code" gorm:"type:varchar(64)"`
	ErrorMessage string `json:"error_message" gorm:"type:varchar(1024)"`
	CheckedAt    int64  `json:"checked_at" gorm:"bigint;index:idx_group_probe_group_checked,priority:2;index:idx_group_probe_checked_at"`
}

func (GroupProbeResult) TableName() string {
	return "group_probe_results"
}

func (result *GroupProbeResult) BeforeSave(_ *gorm.DB) error {
	result.ErrorCode = SanitizeGroupProbeErrorCode(result.ErrorCode)
	result.ErrorMessage = SanitizeGroupProbeError(result.ErrorMessage)
	if result.Success {
		result.ErrorCode = ""
		result.ErrorMessage = ""
	}
	return nil
}

func (result *GroupProbeResult) BeforeCreate(_ *gorm.DB) error {
	if result.CheckedAt == 0 {
		result.CheckedAt = common.GetTimestamp()
	}
	return nil
}

func CreateGroupProbeResult(result *GroupProbeResult) error {
	if result == nil {
		return errors.New("group probe result is nil")
	}
	result.TaskID = strings.TrimSpace(result.TaskID)
	result.GroupName = strings.TrimSpace(result.GroupName)
	result.DisplayName = strings.TrimSpace(result.DisplayName)
	result.ModelName = strings.TrimSpace(result.ModelName)
	if err := validateGroupProbeResult(result); err != nil {
		return err
	}
	return DB.Create(result).Error
}

func DeleteGroupProbeResultsBefore(cutoff int64) (int64, error) {
	if cutoff <= 0 {
		return 0, errors.New("group probe retention cutoff must be positive")
	}
	result := DB.Where("checked_at < ?", cutoff).Delete(&GroupProbeResult{})
	return result.RowsAffected, result.Error
}

func DeleteExpiredGroupProbeResults(retentionDays int, now int64) (int64, error) {
	if retentionDays <= 0 {
		return 0, errors.New("group probe retention_days must be positive")
	}
	if now <= 0 {
		return 0, errors.New("group probe current time must be positive")
	}
	return DeleteGroupProbeResultsBefore(now - int64(retentionDays)*24*60*60)
}

func GetGroupProbeResults(groupName string, start, end int64) ([]GroupProbeResult, error) {
	var results []GroupProbeResult
	query := DB.Model(&GroupProbeResult{}).Where("checked_at >= ? AND checked_at <= ?", start, end)
	if groupName != "" {
		query = query.Where("group_name = ?", groupName)
	}
	err := query.Order("checked_at ASC, id ASC").Find(&results).Error
	return results, err
}

type GroupProbeState string

const (
	GroupProbeStateHealthy  GroupProbeState = "healthy"
	GroupProbeStateDegraded GroupProbeState = "degraded"
	GroupProbeStateDown     GroupProbeState = "down"
	GroupProbeStateUnknown  GroupProbeState = "unknown"
)

type GroupProbeTarget struct {
	GroupName       string
	DisplayName     string
	ModelName       string
	IntervalMinutes int
	TimeoutSeconds  int
}

type GroupProbeBucket struct {
	StartedAt   int64           `json:"started_at"`
	State       GroupProbeState `json:"state"`
	SampleCount int             `json:"sample_count"`
}

type GroupProbePublicStatus struct {
	GroupName        string             `json:"group_name"`
	DisplayName      string             `json:"display_name"`
	ModelName        string             `json:"model_name"`
	State            GroupProbeState    `json:"state"`
	Availability     *float64           `json:"availability"`
	AverageLatencyMS *int64             `json:"average_latency_ms"`
	SampleCount      int                `json:"sample_count"`
	LatestCheckedAt  *int64             `json:"latest_checked_at"`
	Stale            bool               `json:"stale"`
	IntervalMinutes  int                `json:"interval_minutes"`
	Buckets          []GroupProbeBucket `json:"buckets"`
}

func AggregateGroupProbeResults(target GroupProbeTarget, results []GroupProbeResult, now int64) GroupProbePublicStatus {
	target.IntervalMinutes = effectiveGroupProbeIntervalMinutes(target.IntervalMinutes)
	if target.TimeoutSeconds <= 0 {
		target.TimeoutSeconds = 45
	}
	status := GroupProbePublicStatus{
		GroupName:       target.GroupName,
		DisplayName:     target.DisplayName,
		ModelName:       target.ModelName,
		State:           GroupProbeStateUnknown,
		IntervalMinutes: target.IntervalMinutes,
		Buckets:         makeGroupProbeBuckets(now, target.IntervalMinutes),
	}

	groupResults := make([]GroupProbeResult, 0, len(results))
	for _, result := range results {
		if result.GroupName == target.GroupName && result.CheckedAt > 0 && result.CheckedAt <= now {
			groupResults = append(groupResults, result)
		}
	}
	sort.SliceStable(groupResults, func(i, j int) bool {
		if groupResults[i].CheckedAt == groupResults[j].CheckedAt {
			return groupResults[i].ID > groupResults[j].ID
		}
		return groupResults[i].CheckedAt > groupResults[j].CheckedAt
	})
	if len(groupResults) > 0 {
		latest := groupResults[0].CheckedAt
		status.LatestCheckedAt = &latest
		freshnessLimit := int64(2*target.IntervalMinutes*60 + target.TimeoutSeconds)
		status.Stale = latest < now-freshnessLimit
		if !status.Stale {
			freshnessCutoff := now - freshnessLimit
			freshCount := 0
			for _, result := range groupResults {
				if result.CheckedAt < freshnessCutoff {
					break
				}
				freshCount++
			}
			status.State = currentGroupProbeState(groupResults[:freshCount])
		}
	}

	windowStart := now - GroupProbeWindowSeconds
	bucketSeconds := int64(target.IntervalMinutes * 60)
	firstBucket := status.Buckets[0].StartedAt
	successCount := 0
	var successfulLatency int64
	for _, result := range groupResults {
		if result.CheckedAt < windowStart {
			continue
		}
		status.SampleCount++
		if result.Success {
			successCount++
			successfulLatency += result.LatencyMS
		}
		if result.CheckedAt < firstBucket {
			continue
		}
		bucketIndex := int((result.CheckedAt - firstBucket) / bucketSeconds)
		if bucketIndex < 0 || bucketIndex >= len(status.Buckets) {
			continue
		}
		bucket := &status.Buckets[bucketIndex]
		if bucket.SampleCount == 0 {
			if result.Success {
				bucket.State = GroupProbeStateHealthy
			} else {
				bucket.State = GroupProbeStateDown
			}
		} else if (bucket.State == GroupProbeStateHealthy && !result.Success) || (bucket.State == GroupProbeStateDown && result.Success) {
			bucket.State = GroupProbeStateDegraded
		}
		bucket.SampleCount++
	}
	if status.SampleCount > 0 {
		availability := float64(successCount) / float64(status.SampleCount)
		status.Availability = &availability
	}
	if successCount > 0 {
		averageLatency := successfulLatency / int64(successCount)
		status.AverageLatencyMS = &averageLatency
	}
	return status
}

func GetGroupProbePublicStatuses(targets []GroupProbeTarget, now int64) ([]GroupProbePublicStatus, error) {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	statuses := make([]GroupProbePublicStatus, 0, len(targets))
	for _, target := range targets {
		intervalMinutes := effectiveGroupProbeIntervalMinutes(target.IntervalMinutes)
		target.IntervalMinutes = intervalMinutes
		timeoutSeconds := target.TimeoutSeconds
		if timeoutSeconds <= 0 {
			timeoutSeconds = 45
		}
		lookback := int64(2*intervalMinutes*60 + timeoutSeconds)
		if lookback < GroupProbeWindowSeconds {
			lookback = GroupProbeWindowSeconds
		}
		var results []GroupProbeResult
		if err := DB.Where("group_name = ? AND checked_at >= ? AND checked_at <= ?", target.GroupName, now-lookback, now).
			Order("checked_at DESC, id DESC").
			Find(&results).Error; err != nil {
			return nil, err
		}
		statuses = append(statuses, AggregateGroupProbeResults(target, results, now))
	}
	return statuses, nil
}

var (
	groupProbeBodyPattern          = regexp.MustCompile(`(?is)\b(?:upstream\s+|response\s+)?body\s*[:=].*$`)
	groupProbeAuthorizationPattern = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*(?:(?:bearer|basic)\s+)?[^,;\s]+`)
	groupProbeBearerPattern        = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[a-z0-9._~+/=-]+`)
	groupProbeCredentialPattern    = regexp.MustCompile(`(?i)\b(api[_ -]?key|access[_ -]?token|token|secret|password)\b(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^,;\s]+)`)
	groupProbeQueryPattern         = regexp.MustCompile(`([?&][^=\s&]+)=([^&#\s]+)`)
	groupProbeUserInfoPattern      = regexp.MustCompile(`://[^/@\s]+:[^/@\s]+@`)
	groupProbeWhitespacePattern    = regexp.MustCompile(`\s+`)
)

func SanitizeGroupProbeError(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	message = groupProbeBodyPattern.ReplaceAllString(message, "body: [redacted]")
	message = groupProbeAuthorizationPattern.ReplaceAllString(message, "authorization: [redacted]")
	message = groupProbeBearerPattern.ReplaceAllString(message, "[redacted]")
	message = groupProbeCredentialPattern.ReplaceAllString(message, "$1$2[redacted]")
	message = groupProbeQueryPattern.ReplaceAllString(message, "$1[redacted]")
	message = groupProbeUserInfoPattern.ReplaceAllString(message, "://[redacted]@")
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message)
	message = strings.TrimSpace(groupProbeWhitespacePattern.ReplaceAllString(message, " "))
	return truncateRunes(message, MaxGroupProbeErrorMessageLength)
}

func SanitizeGroupProbeErrorCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range code {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && builder.Len() > 0 {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	normalized := strings.Trim(builder.String(), "_")
	if normalized == "" {
		normalized = "probe_failed"
	}
	return truncateRunes(normalized, MaxGroupProbeErrorCodeLength)
}

func validateGroupProbeResult(result *GroupProbeResult) error {
	fields := []struct {
		name     string
		value    string
		limit    int
		required bool
	}{
		{name: "task_id", value: result.TaskID, limit: 64},
		{name: "group_name", value: result.GroupName, limit: 64, required: true},
		{name: "display_name", value: result.DisplayName, limit: 128, required: true},
		{name: "model_name", value: result.ModelName, limit: 255, required: true},
	}
	for _, field := range fields {
		if field.required && field.value == "" {
			return fmt.Errorf("%s must not be empty", field.name)
		}
		if !utf8.ValidString(field.value) || utf8.RuneCountInString(field.value) > field.limit {
			return fmt.Errorf("%s must be valid UTF-8 and contain at most %d characters", field.name, field.limit)
		}
	}
	if result.LatencyMS < 0 {
		return errors.New("latency_ms must not be negative")
	}
	if result.ChannelID != nil && *result.ChannelID <= 0 {
		result.ChannelID = nil
	}
	return nil
}

func currentGroupProbeState(results []GroupProbeResult) GroupProbeState {
	limit := len(results)
	if limit > 3 {
		limit = 3
	}
	if limit == 0 {
		return GroupProbeStateUnknown
	}
	successes := 0
	for i := 0; i < limit; i++ {
		if results[i].Success {
			successes++
		}
	}
	if limit >= 3 && successes == 0 {
		return GroupProbeStateDown
	}
	if !results[0].Success && limit < 3 {
		return GroupProbeStateUnknown
	}
	if results[0].Success && (limit == 1 || successes >= 2) {
		return GroupProbeStateHealthy
	}
	return GroupProbeStateDegraded
}

func effectiveGroupProbeIntervalMinutes(intervalMinutes int) int {
	if intervalMinutes < GroupProbeMinIntervalMinutes || intervalMinutes > GroupProbeMaxIntervalMinutes {
		return GroupProbeDefaultIntervalMinutes
	}
	return intervalMinutes
}

func makeGroupProbeBuckets(now int64, intervalMinutes int) []GroupProbeBucket {
	intervalMinutes = effectiveGroupProbeIntervalMinutes(intervalMinutes)
	bucketSeconds := int64(intervalMinutes * 60)
	bucketCount := (GroupProbeWindowMinutes + intervalMinutes - 1) / intervalMinutes
	if bucketCount > GroupProbeMaxBucketCount {
		bucketCount = GroupProbeMaxBucketCount
	}
	currentBucket := now - now%bucketSeconds
	firstBucket := currentBucket - int64(bucketCount-1)*bucketSeconds
	buckets := make([]GroupProbeBucket, bucketCount)
	for i := range buckets {
		buckets[i] = GroupProbeBucket{
			StartedAt: firstBucket + int64(i)*bucketSeconds,
			State:     GroupProbeStateUnknown,
		}
	}
	return buckets
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}
