package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/group_probe_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGroupProbeControllerTest(t *testing.T) time.Time {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	previousDatabaseType := common.MainDatabaseType()
	previousSetting := group_probe_setting.GetSetting()
	previousClock := groupProbeClock
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()

	dsn := fmt.Sprintf("file:group_probe_controller_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Ability{},
		&model.GroupProbeResult{},
		&model.Option{},
		&model.SystemTask{},
	))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, group_probe_setting.SetSetting(group_probe_setting.DefaultSetting(), nil))
	resetGroupProbePublicCache()
	fixedNow := time.Unix(2_000_001_600, 0)
	groupProbeClock = func() time.Time { return fixedNow }

	t.Cleanup(func() {
		resetGroupProbePublicCache()
		groupProbeClock = previousClock
		require.NoError(t, group_probe_setting.SetSetting(previousSetting, nil))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		model.DB = previousDB
		common.SetMainDatabaseType(previousDatabaseType)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return fixedNow
}

func performGroupProbeControllerRequest(method, target, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		context.Request.Header.Set("Content-Type", "application/json")
	}
	handler(context)
	return response
}

func TestGroupProbePublicStatusFiltersPrivateMappingsAndUsesAllowlist(t *testing.T) {
	now := setupGroupProbeControllerTest(t)
	setting := group_probe_setting.DefaultSetting()
	setting.Enabled = true
	setting.Groups = []group_probe_setting.GroupMapping{
		{Group: "codex", DisplayName: "Codex", Model: "gpt-5.5", Public: true},
		{Group: "private", DisplayName: "Private upstream", Model: "secret-model", Public: false},
	}
	require.NoError(t, group_probe_setting.SetSetting(setting, nil))
	channelID := 17
	require.NoError(t, model.CreateGroupProbeResult(&model.GroupProbeResult{
		TaskID: "systask_public", GroupName: "codex", DisplayName: "Codex", ModelName: "gpt-5.5",
		ChannelID: &channelID, Success: true, LatencyMS: 321, CheckedAt: now.Unix() - 10,
	}))
	require.NoError(t, model.CreateGroupProbeResult(&model.GroupProbeResult{
		TaskID: "systask_private", GroupName: "private", DisplayName: "Private upstream", ModelName: "secret-model",
		ChannelID: &channelID, Success: false, ErrorMessage: "api_key=sk-private", CheckedAt: now.Unix() - 10,
	}))

	response := performGroupProbeControllerRequest(http.MethodGet, "/api/status/probes", "", GetPublicGroupProbeStatus)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "public, max-age=15", response.Header().Get("Cache-Control"))
	var payload struct {
		Success bool                    `json:"success"`
		Message string                  `json:"message"`
		Data    groupProbePublicDataDTO `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	assert.Empty(t, payload.Message)
	require.Len(t, payload.Data.Groups, 1)
	assert.Equal(t, "codex", payload.Data.Groups[0].GroupName)
	assert.Equal(t, now.Unix(), payload.Data.GeneratedAt)
	require.Len(
		t,
		payload.Data.Groups[0].Buckets,
		model.GroupProbeWindowMinutes/model.GroupProbeDefaultIntervalMinutes,
	)

	serialized := response.Body.String()
	for _, forbidden := range []string{
		"private", "secret-model", "channel_id", "task_id", "error_code", "error_message", "sk-private",
	} {
		assert.NotContains(t, serialized, forbidden)
	}
	var rawEnvelope map[string]any
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &rawEnvelope))
	assert.ElementsMatch(t, []string{"success", "message", "data"}, mapKeys(rawEnvelope))
	data := rawEnvelope["data"].(map[string]any)
	assert.ElementsMatch(t, []string{"generated_at", "interval_minutes", "groups"}, mapKeys(data))
	group := data["groups"].([]any)[0].(map[string]any)
	assert.ElementsMatch(t, []string{
		"group_name", "display_name", "model_name", "state", "availability", "average_latency_ms",
		"sample_count", "latest_checked_at", "stale", "interval_minutes", "buckets",
	}, mapKeys(group))
}

func TestGroupProbePublicStatusCachesForFifteenSeconds(t *testing.T) {
	now := setupGroupProbeControllerTest(t)
	setting := group_probe_setting.DefaultSetting()
	setting.Groups = []group_probe_setting.GroupMapping{{Group: "codex", DisplayName: "Codex", Model: "gpt-5.5", Public: true}}
	require.NoError(t, group_probe_setting.SetSetting(setting, nil))
	require.NoError(t, model.CreateGroupProbeResult(&model.GroupProbeResult{
		GroupName: "codex", DisplayName: "Codex", ModelName: "gpt-5.5", Success: true, CheckedAt: now.Unix() - 10,
	}))

	first := performGroupProbeControllerRequest(http.MethodGet, "/api/status/probes", "", GetPublicGroupProbeStatus)
	require.NoError(t, model.CreateGroupProbeResult(&model.GroupProbeResult{
		GroupName: "codex", DisplayName: "Codex", ModelName: "gpt-5.5", Success: false, CheckedAt: now.Unix() - 5,
	}))
	second := performGroupProbeControllerRequest(http.MethodGet, "/api/status/probes", "", GetPublicGroupProbeStatus)
	assert.JSONEq(t, first.Body.String(), second.Body.String())

	groupProbeClock = func() time.Time { return now.Add(16 * time.Second) }
	third := performGroupProbeControllerRequest(http.MethodGet, "/api/status/probes", "", GetPublicGroupProbeStatus)
	var payload struct {
		Data groupProbePublicDataDTO `json:"data"`
	}
	require.NoError(t, common.Unmarshal(third.Body.Bytes(), &payload))
	require.Len(t, payload.Data.Groups, 1)
	assert.Equal(t, 2, payload.Data.Groups[0].SampleCount)
}

func TestGroupProbePublicStatusReturnsSuccessfulEmptyState(t *testing.T) {
	setupGroupProbeControllerTest(t)
	response := performGroupProbeControllerRequest(http.MethodGet, "/api/status/probes", "", GetPublicGroupProbeStatus)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Success bool                    `json:"success"`
		Data    groupProbePublicDataDTO `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	assert.Empty(t, payload.Data.Groups)
}

func TestUpdateGroupProbeSettingsValidatesAbilityAndPersistsOption(t *testing.T) {
	setupGroupProbeControllerTest(t)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "codex", Model: "gpt-5.5", ChannelId: 1, Enabled: true}).Error)
	body := `{"enabled":true,"interval_minutes":10,"retention_days":7,"timeout_seconds":45,"groups":[{"group":" codex ","display_name":" Codex ","model":" gpt-5.5 ","public":true}]}`

	response := performGroupProbeControllerRequest(http.MethodPut, "/api/group-probe/settings", body, UpdateGroupProbeSettings)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Success bool                        `json:"success"`
		Data    group_probe_setting.Setting `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	require.Len(t, payload.Data.Groups, 1)
	assert.Equal(t, "codex", payload.Data.Groups[0].Group)

	var option model.Option
	require.NoError(t, model.DB.Where("key = ?", group_probe_setting.OptionKey).First(&option).Error)
	persisted, err := group_probe_setting.Decode(option.Value, func(group, modelName string) (bool, error) {
		return group == "codex" && modelName == "gpt-5.5", nil
	})
	require.NoError(t, err)
	assert.Equal(t, payload.Data, persisted)
	assert.Equal(t, persisted, group_probe_setting.GetSetting())

	invalid := strings.Replace(body, "gpt-5.5", "missing-model", 1)
	response = performGroupProbeControllerRequest(http.MethodPut, "/api/group-probe/settings", invalid, UpdateGroupProbeSettings)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Contains(t, response.Body.String(), "enabled ability")
	assert.Equal(t, persisted, group_probe_setting.GetSetting())
}

func TestRunGroupProbeReturnsCreatedAndExistingTask(t *testing.T) {
	setupGroupProbeControllerTest(t)
	first := performGroupProbeControllerRequest(http.MethodPost, "/api/group-probe/run", "", RunGroupProbe)
	require.Equal(t, http.StatusOK, first.Code)
	var firstPayload struct {
		Success bool             `json:"success"`
		Data    groupProbeRunDTO `json:"data"`
	}
	require.NoError(t, common.Unmarshal(first.Body.Bytes(), &firstPayload))
	assert.True(t, firstPayload.Success)
	assert.True(t, firstPayload.Data.Created)
	assert.NotEmpty(t, firstPayload.Data.TaskID)

	second := performGroupProbeControllerRequest(http.MethodPost, "/api/group-probe/run", "", RunGroupProbe)
	var secondPayload struct {
		Data groupProbeRunDTO `json:"data"`
	}
	require.NoError(t, common.Unmarshal(second.Body.Bytes(), &secondPayload))
	assert.False(t, secondPayload.Data.Created)
	assert.Equal(t, firstPayload.Data.TaskID, secondPayload.Data.TaskID)
	var task model.SystemTask
	require.NoError(t, model.DB.Where("task_id = ?", firstPayload.Data.TaskID).First(&task).Error)
	assert.Equal(t, model.SystemTaskTypeGroupProbe, task.Type)
}

func TestGetGroupProbeResultsFiltersCapsPaginationAndSanitizes(t *testing.T) {
	now := setupGroupProbeControllerTest(t)
	channelID := 9
	rows := make([]model.GroupProbeResult, 0, 106)
	for index := 0; index < 105; index++ {
		rows = append(rows, model.GroupProbeResult{
			TaskID: "task?unsafe", GroupName: "codex", DisplayName: "Codex", ModelName: "gpt-5.5",
			ChannelID: &channelID, Success: false, LatencyMS: int64(index), ErrorCode: "Timeout Error",
			ErrorMessage: "authorization: Bearer sk-private body: upstream-secret", CheckedAt: now.Unix() - int64(index),
		})
	}
	rows = append(rows, model.GroupProbeResult{
		TaskID: "private-task", GroupName: "private", DisplayName: "Private", ModelName: "secret", Success: true, CheckedAt: now.Unix(),
	})
	require.NoError(t, model.DB.Session(&gorm.Session{SkipHooks: true}).Create(&rows).Error)

	response := performGroupProbeControllerRequest(
		http.MethodGet,
		"/api/group-probe/results?page=1&page_size=1000&group=codex&success=false",
		"",
		GetGroupProbeResults,
	)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Page     int                        `json:"page"`
			PageSize int                        `json:"page_size"`
			Total    int64                      `json:"total"`
			Items    []groupProbeAdminResultDTO `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	assert.Equal(t, 100, payload.Data.PageSize)
	assert.Equal(t, int64(105), payload.Data.Total)
	require.Len(t, payload.Data.Items, 100)
	for _, item := range payload.Data.Items {
		assert.Equal(t, "codex", item.GroupName)
		assert.False(t, item.Success)
		assert.Equal(t, "task_unsafe", item.TaskID)
		assert.Equal(t, "timeout_error", item.ErrorCode)
		assert.NotContains(t, item.ErrorMessage, "sk-private")
		assert.NotContains(t, item.ErrorMessage, "upstream-secret")
		require.NotNil(t, item.ChannelID)
		assert.Equal(t, channelID, *item.ChannelID)
	}

	invalidSuccess := performGroupProbeControllerRequest(http.MethodGet, "/api/group-probe/results?success=maybe", "", GetGroupProbeResults)
	assert.Equal(t, http.StatusBadRequest, invalidSuccess.Code)
	invalidPage := performGroupProbeControllerRequest(http.MethodGet, "/api/group-probe/results?page=-1", "", GetGroupProbeResults)
	assert.Equal(t, http.StatusBadRequest, invalidPage.Code)
	longGroup := strings.Repeat("g", group_probe_setting.MaxGroupNameLength+1)
	invalidGroup := performGroupProbeControllerRequest(http.MethodGet, "/api/group-probe/results?group="+longGroup, "", GetGroupProbeResults)
	assert.Equal(t, http.StatusBadRequest, invalidGroup.Code)

	successResponse := performGroupProbeControllerRequest(http.MethodGet, "/api/group-probe/results?group=private&success=true", "", GetGroupProbeResults)
	var successPayload struct {
		Data struct {
			Items []groupProbeAdminResultDTO `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(successResponse.Body.Bytes(), &successPayload))
	require.Len(t, successPayload.Data.Items, 1)
	assert.Empty(t, successPayload.Data.Items[0].ErrorCode)
	assert.Empty(t, successPayload.Data.Items[0].ErrorMessage)
}

func TestUpdateGroupProbeSettingsRejectsMalformedJSON(t *testing.T) {
	setupGroupProbeControllerTest(t)
	response := performGroupProbeControllerRequest(http.MethodPut, "/api/group-probe/settings", `{"enabled":`, UpdateGroupProbeSettings)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.JSONEq(t, `{"success":false,"message":"invalid group probe settings"}`, response.Body.String())
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
