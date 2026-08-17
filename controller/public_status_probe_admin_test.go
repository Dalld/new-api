package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPublicStatusProbeAdminControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	return db
}

func preservePublicStatusProbeAdminRuntime(t *testing.T) {
	t.Helper()

	previousDocument := publicstatusprobesetting.CurrentDocument()
	previousHook := publicstatusprobesetting.SetPublishHook(nil)
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	clonedOptionMap := make(map[string]string, len(previousOptionMap))
	for key, value := range previousOptionMap {
		clonedOptionMap[key] = value
	}
	common.OptionMap = clonedOptionMap
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		require.NoError(t, publicstatusprobesetting.PublishDocument(previousDocument))
		publicstatusprobesetting.SetPublishHook(previousHook)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})
}

func seedPublicStatusProbeAdminUser(t *testing.T, db *gorm.DB, username string, role int, token string) *model.User {
	t.Helper()

	user := &model.User{
		Username: username,
		Password: "password123",
		Role:     role,
		Status:   common.UserStatusEnabled,
		AffCode:  username + "-aff-code",
	}
	user.SetAccessToken(token)
	require.NoError(t, db.Create(user).Error)
	return user
}

func seedPublicStatusProbeAdminChannel(t *testing.T, db *gorm.DB, channel model.Channel) model.Channel {
	t.Helper()
	if channel.Status == 0 {
		channel.Status = common.ChannelStatusEnabled
	}
	if channel.Type == 0 {
		channel.Type = constant.ChannelTypeOpenAI
	}
	if channel.Key == "" {
		channel.Key = "provider-key"
	}
	if channel.Name == "" {
		channel.Name = "provider-name"
	}
	if channel.Models == "" {
		channel.Models = "gpt-5.5"
	}
	require.NoError(t, db.Create(&channel).Error)
	return channel
}

func publishPublicStatusProbeAdminConfig(t *testing.T, db *gorm.DB, document publicstatusprobesetting.Document) publicstatusprobesetting.Document {
	t.Helper()

	saved, created, err := model.EnsurePublicStatusProbeConfig(document)
	require.NoError(t, err)
	assert.True(t, created)
	require.NoError(t, publicstatusprobesetting.PublishDocument(saved))
	return saved
}

func performPublicStatusProbeAdminRequest(t *testing.T, engine *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func registerPublicStatusProbeAdminTestRoutes(engine *gin.Engine) {
	admin := engine.Group("/api/public-status-probe")
	admin.Use(middleware.DisableCache(), middleware.RootAuth())
	{
		admin.GET("/config", GetPublicStatusProbeConfig)
		admin.PUT("/config", UpdatePublicStatusProbeConfig)
		admin.POST("/targets", CreatePublicStatusProbeTarget)
		admin.PUT("/targets/:key", UpdatePublicStatusProbeTarget)
		admin.DELETE("/targets/:key", DeletePublicStatusProbeTarget)
	}
}

func TestPublicStatusProbeAdminRoutesRequireRootAndDisableCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupPublicStatusProbeAdminControllerTestDB(t)
	preservePublicStatusProbeAdminRuntime(t)

	rootToken := "public-status-probe-root-token"
	adminToken := "public-status-probe-admin-token"
	seedPublicStatusProbeAdminUser(t, db, "root", common.RoleRootUser, rootToken)
	seedPublicStatusProbeAdminUser(t, db, "admin", common.RoleAdminUser, adminToken)

	engine := gin.New()
	registerPublicStatusProbeAdminTestRoutes(engine)
	routes := map[string]struct{}{}
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		http.MethodGet + " /api/public-status-probe/config",
		http.MethodPut + " /api/public-status-probe/config",
		http.MethodPost + " /api/public-status-probe/targets",
		http.MethodPut + " /api/public-status-probe/targets/:key",
		http.MethodDelete + " /api/public-status-probe/targets/:key",
	} {
		_, exists := routes[route]
		assert.True(t, exists, "missing route %s", route)
	}

	anonymous := performPublicStatusProbeAdminRequest(t, engine, http.MethodGet, "/api/public-status-probe/config", "", "")
	assert.Equal(t, http.StatusUnauthorized, anonymous.Code)
	assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", anonymous.Header().Get("Cache-Control"))

	admin := performPublicStatusProbeAdminRequest(t, engine, http.MethodGet, "/api/public-status-probe/config", "", adminToken)
	assert.Equal(t, http.StatusForbidden, admin.Code)
	assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", admin.Header().Get("Cache-Control"))

	root := performPublicStatusProbeAdminRequest(t, engine, http.MethodGet, "/api/public-status-probe/config", "", rootToken)
	assert.Equal(t, http.StatusOK, root.Code)
	assert.Equal(t, "no-store, no-cache, must-revalidate, private, max-age=0", root.Header().Get("Cache-Control"))
}

func TestPublicStatusProbeAdminCRUDAndStrictValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupPublicStatusProbeAdminControllerTestDB(t)
	preservePublicStatusProbeAdminRuntime(t)

	rootToken := "public-status-probe-root-crud-token"
	seedPublicStatusProbeAdminUser(t, db, "root-crud", common.RoleRootUser, rootToken)
	safeChannel := seedPublicStatusProbeAdminChannel(t, db, model.Channel{
		Id:           42,
		Name:         "Safe Channel",
		Key:          "channel-secret-marker",
		Type:         constant.ChannelTypeOpenAI,
		Models:       "gpt-5.5,gpt-5.5-mini",
		BaseURL:      ptrString("https://private.example.invalid"),
		ModelMapping: ptrString(`{"gpt-5.5":"upstream-gpt-5.5"}`),
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled},
		},
	})

	initial := publicstatusprobesetting.DefaultDocument()
	initial.Enabled = true
	initial.Version = 7
	initial.Targets = []publicstatusprobesetting.Target{
		{
			Enabled:     true,
			Key:         "probe-a",
			Group:       "group-a",
			DisplayName: "Probe A",
			Model:       "gpt-5.5",
			Protocol:    publicstatusprobesetting.ProtocolOpenAIChat,
			ChannelID:   safeChannel.Id,
			KeyIndex:    1,
		},
	}
	publishPublicStatusProbeAdminConfig(t, db, initial)

	engine := gin.New()
	registerPublicStatusProbeAdminTestRoutes(engine)

	getResponse := performPublicStatusProbeAdminRequest(t, engine, http.MethodGet, "/api/public-status-probe/config", "", rootToken)
	require.Equal(t, http.StatusOK, getResponse.Code)
	var getPayload publicStatusProbeAdminResponse
	require.NoError(t, common.Unmarshal(getResponse.Body.Bytes(), &getPayload))
	require.True(t, getPayload.Success)
	assert.Equal(t, initial.Version, getPayload.Data.Version)
	require.Len(t, getPayload.Data.Targets, 1)
	require.Equal(t, safeChannel.Id, getPayload.Data.Targets[0].Channel.ID)
	assert.Equal(t, "Safe Channel", getPayload.Data.Targets[0].Channel.Name)
	assert.Equal(t, "gpt-5.5,gpt-5.5-mini", getPayload.Data.Targets[0].Channel.Models)
	assert.Equal(t, 2, getPayload.Data.Targets[0].Channel.KeyCount)
	for _, forbidden := range []string{"channel-secret-marker", "private.example.invalid", "upstream-gpt-5.5", "base_url", "model_mapping"} {
		assert.NotContains(t, getResponse.Body.String(), forbidden)
	}

	createBody := `{"version":7,"enabled":true,"group":"group-b","display_name":"Probe B","model":"gpt-5.5-mini","protocol":"openai_chat","channel_id":42,"key_index":0,"key":"client-supplied"}`
	createResponse := performPublicStatusProbeAdminRequest(t, engine, http.MethodPost, "/api/public-status-probe/targets", createBody, rootToken)
	require.Equal(t, http.StatusOK, createResponse.Code)
	var createPayload publicStatusProbeAdminResponse
	require.NoError(t, common.Unmarshal(createResponse.Body.Bytes(), &createPayload))
	require.Len(t, createPayload.Data.Targets, 2)
	assert.NotEqual(t, "client-supplied", createPayload.Data.Targets[1].Key)
	assert.True(t, strings.HasPrefix(createPayload.Data.Targets[1].Key, "probe-"))
	assert.Equal(t, "Probe B", createPayload.Data.Targets[1].DisplayName)
	assert.NotContains(t, createResponse.Body.String(), "client-supplied")

	targetKey := createPayload.Data.Targets[1].Key
	updateBody := `{"version":8,"enabled":false,"group":"group-c","display_name":"Probe C","model":"gpt-5.5","protocol":"openai_chat","channel_id":42,"key_index":0,"key":"ignored-by-server"}`
	updateResponse := performPublicStatusProbeAdminRequest(t, engine, http.MethodPut, "/api/public-status-probe/targets/"+targetKey, updateBody, rootToken)
	require.Equal(t, http.StatusOK, updateResponse.Code)
	var updatePayload publicStatusProbeAdminResponse
	require.NoError(t, common.Unmarshal(updateResponse.Body.Bytes(), &updatePayload))
	require.Len(t, updatePayload.Data.Targets, 2)
	assert.Equal(t, targetKey, updatePayload.Data.Targets[1].Key)
	assert.False(t, updatePayload.Data.Targets[1].Enabled)
	assert.Equal(t, "group-c", updatePayload.Data.Targets[1].Group)
	assert.NotContains(t, updateResponse.Body.String(), "ignored-by-server")

	unknownFieldResponse := performPublicStatusProbeAdminRequest(t, engine, http.MethodPost, "/api/public-status-probe/targets", `{"version":9,"enabled":true,"group":"x","display_name":"x","model":"gpt-5.5","protocol":"openai_chat","channel_id":42,"key_index":0,"secret":"x"}`, rootToken)
	assert.Equal(t, http.StatusBadRequest, unknownFieldResponse.Code)

	oversizedResponse := performPublicStatusProbeAdminRequest(t, engine, http.MethodPut, "/api/public-status-probe/config", `{"version":9,"enabled":true,"ping_timeout_seconds":8,"chat_timeout_seconds":45,"degraded_latency_ms":6000,"concurrency":5,"retention_days":7}`+strings.Repeat(" ", 70*1024), rootToken)
	assert.Equal(t, http.StatusBadRequest, oversizedResponse.Code)

	staleDelete := performPublicStatusProbeAdminRequest(t, engine, http.MethodDelete, "/api/public-status-probe/targets/"+targetKey+"?version=7", "", rootToken)
	assert.Equal(t, http.StatusConflict, staleDelete.Code)

	deleteResponse := performPublicStatusProbeAdminRequest(t, engine, http.MethodDelete, "/api/public-status-probe/targets/"+targetKey+"?version=9", "", rootToken)
	require.Equal(t, http.StatusOK, deleteResponse.Code)
	var deletePayload publicStatusProbeAdminResponse
	require.NoError(t, common.Unmarshal(deleteResponse.Body.Bytes(), &deletePayload))
	require.Len(t, deletePayload.Data.Targets, 1)
	assert.Equal(t, "probe-a", deletePayload.Data.Targets[0].Key)
}

func TestPublicStatusProbeAdminRejectsMissingTargetAndStaleConfigVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupPublicStatusProbeAdminControllerTestDB(t)
	preservePublicStatusProbeAdminRuntime(t)

	rootToken := "public-status-probe-root-conflict-token"
	seedPublicStatusProbeAdminUser(t, db, "root-conflict", common.RoleRootUser, rootToken)
	channel := seedPublicStatusProbeAdminChannel(t, db, model.Channel{
		Id:     71,
		Name:   "Conflict Channel",
		Key:    "conflict-secret-marker",
		Type:   constant.ChannelTypeOpenAI,
		Models: "gpt-5.5",
	})
	initial := publicstatusprobesetting.DefaultDocument()
	initial.Enabled = true
	initial.Version = 12
	initial.Targets = []publicstatusprobesetting.Target{
		{
			Enabled:     true,
			Key:         "probe-conflict",
			Group:       "group-a",
			DisplayName: "Probe Conflict",
			Model:       "gpt-5.5",
			Protocol:    publicstatusprobesetting.ProtocolOpenAIChat,
			ChannelID:   channel.Id,
			KeyIndex:    0,
		},
	}
	publishPublicStatusProbeAdminConfig(t, db, initial)

	engine := gin.New()
	registerPublicStatusProbeAdminTestRoutes(engine)

	missing := performPublicStatusProbeAdminRequest(t, engine, http.MethodPut, "/api/public-status-probe/targets/missing", `{"version":12,"enabled":true,"group":"group-z","display_name":"Missing","model":"gpt-5.5","protocol":"openai_chat","channel_id":404,"key_index":0}`, rootToken)
	assert.Equal(t, http.StatusNotFound, missing.Code)

	staleCreate := performPublicStatusProbeAdminRequest(t, engine, http.MethodPost, "/api/public-status-probe/targets", `{"version":11,"enabled":true,"group":"group-z","display_name":"Stale Create","model":"gpt-5.5","protocol":"openai_chat","channel_id":404,"key_index":0}`, rootToken)
	assert.Equal(t, http.StatusConflict, staleCreate.Code)
	var staleCreatePayload publicStatusProbeAdminConflictResponse
	require.NoError(t, common.Unmarshal(staleCreate.Body.Bytes(), &staleCreatePayload))
	assert.Equal(t, int64(12), staleCreatePayload.Data.Version)

	staleUpdate := performPublicStatusProbeAdminRequest(t, engine, http.MethodPut, "/api/public-status-probe/targets/probe-conflict", `{"version":11,"enabled":true,"group":"group-z","display_name":"Stale Update","model":"gpt-5.5","protocol":"openai_chat","channel_id":404,"key_index":0}`, rootToken)
	assert.Equal(t, http.StatusConflict, staleUpdate.Code)
	var auditLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).Order("id desc").First(&auditLog).Error)
	assert.NotContains(t, auditLog.Other, "Stale Update")
	var auditPayload struct {
		Op struct {
			Action string                 `json:"action"`
			Params map[string]interface{} `json:"params"`
		} `json:"op"`
	}
	require.NoError(t, common.Unmarshal([]byte(auditLog.Other), &auditPayload))
	assert.Equal(t, "public_status_probe.target_update", auditPayload.Op.Action)
	assert.Equal(t, "probe-conflict", auditPayload.Op.Params["target_key"])
	assert.Equal(t, float64(11), auditPayload.Op.Params["version"])
	assert.Equal(t, false, auditPayload.Op.Params["success"])

	conflict := performPublicStatusProbeAdminRequest(t, engine, http.MethodPut, "/api/public-status-probe/config", `{"version":11,"enabled":false,"ping_timeout_seconds":8,"chat_timeout_seconds":45,"degraded_latency_ms":6000,"concurrency":5,"retention_days":7}`, rootToken)
	assert.Equal(t, http.StatusConflict, conflict.Code)
	var conflictPayload publicStatusProbeAdminConflictResponse
	require.NoError(t, common.Unmarshal(conflict.Body.Bytes(), &conflictPayload))
	assert.Equal(t, int64(12), conflictPayload.Data.Version)
}

func TestPublicStatusProbeAdminReturnsAndDeletesOrphanTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupPublicStatusProbeAdminControllerTestDB(t)
	preservePublicStatusProbeAdminRuntime(t)

	rootToken := "public-status-probe-root-orphan-token"
	seedPublicStatusProbeAdminUser(t, db, "root-orphan", common.RoleRootUser, rootToken)
	initial := publicstatusprobesetting.DefaultDocument()
	initial.Enabled = true
	initial.Version = 21
	initial.Targets = []publicstatusprobesetting.Target{
		{
			Enabled:     true,
			Key:         "probe-orphan",
			Group:       "group-orphan",
			DisplayName: "Orphan Probe",
			Model:       "gpt-5.5",
			Protocol:    publicstatusprobesetting.ProtocolOpenAIChat,
			ChannelID:   909,
			KeyIndex:    0,
		},
	}
	publishPublicStatusProbeAdminConfig(t, db, initial)

	engine := gin.New()
	registerPublicStatusProbeAdminTestRoutes(engine)

	getResponse := performPublicStatusProbeAdminRequest(t, engine, http.MethodGet, "/api/public-status-probe/config", "", rootToken)
	require.Equal(t, http.StatusOK, getResponse.Code)
	var getPayload publicStatusProbeAdminResponse
	require.NoError(t, common.Unmarshal(getResponse.Body.Bytes(), &getPayload))
	require.Len(t, getPayload.Data.Targets, 1)
	placeholder := getPayload.Data.Targets[0].Channel
	assert.Equal(t, 909, placeholder.ID)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, placeholder.Status)
	assert.Empty(t, placeholder.Name)
	assert.Zero(t, placeholder.Type)
	assert.Empty(t, placeholder.Models)
	assert.False(t, placeholder.IsMultiKey)
	assert.Zero(t, placeholder.KeyCount)

	deleteResponse := performPublicStatusProbeAdminRequest(t, engine, http.MethodDelete, "/api/public-status-probe/targets/probe-orphan?version=21", "", rootToken)
	require.Equal(t, http.StatusOK, deleteResponse.Code)
	var deletePayload publicStatusProbeAdminResponse
	require.NoError(t, common.Unmarshal(deleteResponse.Body.Bytes(), &deletePayload))
	assert.Equal(t, int64(22), deletePayload.Data.Version)
	assert.Empty(t, deletePayload.Data.Targets)
}

func ptrString(value string) *string {
	return &value
}
