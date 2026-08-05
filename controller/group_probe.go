package controller

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/group_probe_setting"
	"github.com/gin-gonic/gin"
)

const (
	groupProbePublicCacheSeconds = 15
	groupProbeResultMaxPageSize  = 100
	groupProbeResultDefaultSize  = 20
)

type groupProbePublicBucketDTO struct {
	StartedAt   int64                 `json:"started_at"`
	State       model.GroupProbeState `json:"state"`
	SampleCount int                   `json:"sample_count"`
}

type groupProbePublicStatusDTO struct {
	GroupName        string                      `json:"group_name"`
	DisplayName      string                      `json:"display_name"`
	ModelName        string                      `json:"model_name"`
	State            model.GroupProbeState       `json:"state"`
	Availability     *float64                    `json:"availability"`
	AverageLatencyMS *int64                      `json:"average_latency_ms"`
	SampleCount      int                         `json:"sample_count"`
	LatestCheckedAt  *int64                      `json:"latest_checked_at"`
	Stale            bool                        `json:"stale"`
	IntervalMinutes  int                         `json:"interval_minutes"`
	Buckets          []groupProbePublicBucketDTO `json:"buckets"`
}

type groupProbePublicDataDTO struct {
	GeneratedAt     int64                       `json:"generated_at"`
	IntervalMinutes int                         `json:"interval_minutes"`
	Groups          []groupProbePublicStatusDTO `json:"groups"`
}

type groupProbeAdminResultDTO struct {
	ID           int64  `json:"id"`
	TaskID       string `json:"task_id"`
	GroupName    string `json:"group_name"`
	DisplayName  string `json:"display_name"`
	ModelName    string `json:"model_name"`
	ChannelID    *int   `json:"channel_id"`
	Success      bool   `json:"success"`
	LatencyMS    int64  `json:"latency_ms"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
	CheckedAt    int64  `json:"checked_at"`
}

type groupProbeRunDTO struct {
	TaskID  string `json:"task_id"`
	Created bool   `json:"created"`
}

type groupProbePublicCacheEntry struct {
	data      groupProbePublicDataDTO
	expiresAt time.Time
}

var (
	groupProbePublicCacheMu sync.Mutex
	groupProbePublicCache   groupProbePublicCacheEntry
	groupProbeClock         = time.Now
)

func GetPublicGroupProbeStatus(c *gin.Context) {
	now := groupProbeClock()
	c.Header("Cache-Control", "public, max-age=15")

	groupProbePublicCacheMu.Lock()
	if !groupProbePublicCache.expiresAt.IsZero() && now.Before(groupProbePublicCache.expiresAt) {
		data := groupProbePublicCache.data
		groupProbePublicCacheMu.Unlock()
		common.ApiSuccess(c, data)
		return
	}
	groupProbePublicCacheMu.Unlock()

	setting := group_probe_setting.GetSetting()
	targets := make([]model.GroupProbeTarget, 0, len(setting.Groups))
	for _, mapping := range setting.Groups {
		if !mapping.Public {
			continue
		}
		targets = append(targets, model.GroupProbeTarget{
			GroupName:       mapping.Group,
			DisplayName:     mapping.DisplayName,
			ModelName:       mapping.Model,
			IntervalMinutes: setting.IntervalMinutes,
			TimeoutSeconds:  setting.TimeoutSeconds,
		})
	}

	generatedAt := now.Unix()
	statuses, err := model.GetGroupProbePublicStatuses(targets, generatedAt)
	if err != nil {
		groupProbeError(c, http.StatusInternalServerError, "failed to load group probe status")
		return
	}
	data := groupProbePublicDataDTO{
		GeneratedAt:     generatedAt,
		IntervalMinutes: setting.IntervalMinutes,
		Groups:          make([]groupProbePublicStatusDTO, 0, len(statuses)),
	}
	for _, status := range statuses {
		data.Groups = append(data.Groups, newGroupProbePublicStatusDTO(status))
	}

	groupProbePublicCacheMu.Lock()
	groupProbePublicCache = groupProbePublicCacheEntry{
		data:      data,
		expiresAt: now.Add(groupProbePublicCacheSeconds * time.Second),
	}
	groupProbePublicCacheMu.Unlock()
	common.ApiSuccess(c, data)
}

func GetGroupProbeSettings(c *gin.Context) {
	common.ApiSuccess(c, group_probe_setting.GetSetting())
}

func UpdateGroupProbeSettings(c *gin.Context) {
	var candidate group_probe_setting.Setting
	if err := common.DecodeJson(c.Request.Body, &candidate); err != nil {
		groupProbeError(c, http.StatusBadRequest, "invalid group probe settings")
		return
	}

	normalized, err := group_probe_setting.ValidateAndNormalize(candidate, enabledGroupProbeAbility)
	if err != nil {
		groupProbeError(c, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := group_probe_setting.Encode(normalized)
	if err != nil {
		groupProbeError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := model.UpdateOption(group_probe_setting.OptionKey, raw); err != nil {
		groupProbeError(c, http.StatusInternalServerError, "failed to persist group probe settings")
		return
	}

	resetGroupProbePublicCache()
	common.ApiSuccess(c, group_probe_setting.GetSetting())
}

func RunGroupProbe(c *gin.Context) {
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeGroupProbe, map[string]bool{"manual": true})
	if err != nil {
		groupProbeError(c, http.StatusInternalServerError, "failed to enqueue group probe")
		return
	}
	common.ApiSuccess(c, groupProbeRunDTO{TaskID: sanitizeGroupProbeTaskID(task.TaskID), Created: created})
}

func GetGroupProbeResults(c *gin.Context) {
	page, pageSize, err := parseGroupProbePagination(c)
	if err != nil {
		groupProbeError(c, http.StatusBadRequest, err.Error())
		return
	}

	groupName := strings.TrimSpace(c.Query("group"))
	if utf8.RuneCountInString(groupName) > group_probe_setting.MaxGroupNameLength {
		groupProbeError(c, http.StatusBadRequest, "group filter is too long")
		return
	}
	query := model.DB.Model(&model.GroupProbeResult{})
	if groupName != "" {
		query = query.Where("group_name = ?", groupName)
	}
	if rawSuccess, present := c.GetQuery("success"); present {
		success, parseErr := strconv.ParseBool(rawSuccess)
		if parseErr != nil {
			groupProbeError(c, http.StatusBadRequest, "success filter must be true or false")
			return
		}
		query = query.Where("success = ?", success)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		groupProbeError(c, http.StatusInternalServerError, "failed to count group probe results")
		return
	}
	var rows []model.GroupProbeResult
	if err := query.Order("checked_at DESC, id DESC").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&rows).Error; err != nil {
		groupProbeError(c, http.StatusInternalServerError, "failed to load group probe results")
		return
	}
	items := make([]groupProbeAdminResultDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, newGroupProbeAdminResultDTO(row))
	}
	common.ApiSuccess(c, gin.H{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     items,
	})
}

func enabledGroupProbeAbility(group, modelName string) (bool, error) {
	var count int64
	err := model.DB.Model(&model.Ability{}).
		Where(&model.Ability{Group: group, Model: modelName, Enabled: true}).
		Count(&count).Error
	return count > 0, err
}

func newGroupProbePublicStatusDTO(status model.GroupProbePublicStatus) groupProbePublicStatusDTO {
	buckets := make([]groupProbePublicBucketDTO, 0, len(status.Buckets))
	for _, bucket := range status.Buckets {
		buckets = append(buckets, groupProbePublicBucketDTO{
			StartedAt:   bucket.StartedAt,
			State:       bucket.State,
			SampleCount: bucket.SampleCount,
		})
	}
	return groupProbePublicStatusDTO{
		GroupName:        status.GroupName,
		DisplayName:      status.DisplayName,
		ModelName:        status.ModelName,
		State:            status.State,
		Availability:     status.Availability,
		AverageLatencyMS: status.AverageLatencyMS,
		SampleCount:      status.SampleCount,
		LatestCheckedAt:  status.LatestCheckedAt,
		Stale:            status.Stale,
		IntervalMinutes:  status.IntervalMinutes,
		Buckets:          buckets,
	}
}

func newGroupProbeAdminResultDTO(row model.GroupProbeResult) groupProbeAdminResultDTO {
	var channelID *int
	if row.ChannelID != nil && *row.ChannelID > 0 {
		value := *row.ChannelID
		channelID = &value
	}
	errorCode := ""
	errorMessage := ""
	if !row.Success {
		errorCode = model.SanitizeGroupProbeErrorCode(row.ErrorCode)
		errorMessage = model.SanitizeGroupProbeError(row.ErrorMessage)
	}
	return groupProbeAdminResultDTO{
		ID:           row.ID,
		TaskID:       sanitizeGroupProbeTaskID(row.TaskID),
		GroupName:    strings.TrimSpace(row.GroupName),
		DisplayName:  strings.TrimSpace(row.DisplayName),
		ModelName:    strings.TrimSpace(row.ModelName),
		ChannelID:    channelID,
		Success:      row.Success,
		LatencyMS:    row.LatencyMS,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
		CheckedAt:    row.CheckedAt,
	}
}

func parseGroupProbePagination(c *gin.Context) (int, int, error) {
	page, err := parsePositiveGroupProbeInt(firstNonEmpty(c.Query("page"), c.Query("p")), 1)
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := parsePositiveGroupProbeInt(c.Query("page_size"), groupProbeResultDefaultSize)
	if err != nil {
		return 0, 0, err
	}
	if pageSize > groupProbeResultMaxPageSize {
		pageSize = groupProbeResultMaxPageSize
	}
	if page > int(^uint(0)>>1)/pageSize {
		return 0, 0, &groupProbeValidationError{message: "page is too large"}
	}
	return page, pageSize, nil
}

func parsePositiveGroupProbeInt(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value <= 0 {
		return 0, &groupProbeValidationError{message: "pagination values must be positive integers"}
	}
	return int(value), nil
}

type groupProbeValidationError struct {
	message string
}

func (err *groupProbeValidationError) Error() string {
	return err.message
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sanitizeGroupProbeTaskID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	var builder strings.Builder
	for _, char := range taskID {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' {
			builder.WriteRune(char)
		} else {
			builder.WriteByte('_')
		}
		if builder.Len() >= 64 {
			break
		}
	}
	return builder.String()
}

func resetGroupProbePublicCache() {
	groupProbePublicCacheMu.Lock()
	groupProbePublicCache = groupProbePublicCacheEntry{}
	groupProbePublicCacheMu.Unlock()
}

func groupProbeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
}
