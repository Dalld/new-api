package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAffiliateBindRouteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.SetMainDatabaseType(previousDatabaseType)
		common.SetLogDatabaseType(previousLogDatabaseType)
		common.RedisEnabled = previousRedisEnabled
	})
	return db
}

func TestGroupProbeRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	routes := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		http.MethodGet + " /api/status/probes",
		http.MethodGet + " /api/group-probe/settings",
		http.MethodPut + " /api/group-probe/settings",
		http.MethodPost + " /api/group-probe/run",
		http.MethodGet + " /api/group-probe/results",
		http.MethodPost + " /api/affiliate/bind",
		http.MethodGet + " /api/user/aff/overview",
	} {
		_, exists := routes[route]
		assert.True(t, exists, "missing route %s", route)
	}
}

func TestAffiliateOverviewRouteRejectsAnonymousRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	request := httptest.NewRequest(http.MethodGet, "/api/user/aff/overview", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.NotContains(t, response.Body.String(), `"success":true`)
}

func TestAffiliateBindRouteRejectsAnonymousRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	request := httptest.NewRequest(http.MethodPost, "/api/affiliate/bind", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.NotContains(t, response.Body.String(), `"success":true`)
}

func TestAffiliateBindRouteRejectsRegularAdministrator(t *testing.T) {
	db := setupAffiliateBindRouteTestDB(t)
	accessToken := "affiliate-bind-regular-admin-token"
	admin := model.User{
		Username:    "affiliate-bind-admin",
		Password:    "password123",
		AffCode:     "affiliate-bind-admin-code",
		Role:        common.RoleAdminUser,
		Status:      common.UserStatusEnabled,
		AccessToken: &accessToken,
	}
	require.NoError(t, db.Create(&admin).Error)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	request := httptest.NewRequest(http.MethodPost, "/api/affiliate/bind", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_INSUFFICIENT_PRIVILEGE")
}

func TestAffiliateBindRouteRejectsRegularUser(t *testing.T) {
	db := setupAffiliateBindRouteTestDB(t)
	accessToken := "affiliate-bind-regular-user-token"
	user := model.User{
		Username:    "affiliate-bind-user",
		Password:    "password123",
		AffCode:     "affiliate-bind-user-code",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AccessToken: &accessToken,
	}
	require.NoError(t, db.Create(&user).Error)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	request := httptest.NewRequest(http.MethodPost, "/api/affiliate/bind", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_INSUFFICIENT_PRIVILEGE")
}

func TestAffiliateBindRouteAllowsRootAdministrator(t *testing.T) {
	db := setupAffiliateBindRouteTestDB(t)
	accessToken := "affiliate-bind-root-token"
	root := model.User{
		Username:    "affiliate-bind-root",
		Password:    "password123",
		AffCode:     "affiliate-bind-root-code",
		Role:        common.RoleRootUser,
		Status:      common.UserStatusEnabled,
		AccessToken: &accessToken,
	}
	invitee := model.User{
		Username: "affiliate-bind-route-invitee",
		Password: "password123",
		AffCode:  "affiliate-bind-route-invitee-code",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	inviter := model.User{
		Username: "affiliate-bind-route-inviter",
		Password: "password123",
		AffCode:  "affiliate-bind-route-inviter-code",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	for _, user := range []*model.User{&root, &invitee, &inviter} {
		require.NoError(t, db.Create(user).Error)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	body := `{"invitee_id":` + strconv.Itoa(invitee.Id) + `,"inviter_id":` + strconv.Itoa(inviter.Id) + `}`
	request := httptest.NewRequest(http.MethodPost, "/api/affiliate/bind", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"success":true`)
}

func TestGroupProbePublicRouteRequiresNoAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	request := httptest.NewRequest(http.MethodGet, "/api/status/probes", nil)
	response := httptest.NewRecorder()
	require.NotPanics(t, func() { engine.ServeHTTP(response, request) })
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"success":true`)
}

func TestGroupProbeAdminRoutesRejectAnonymousRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/group-probe/settings"},
		{http.MethodPut, "/api/group-probe/settings"},
		{http.MethodPost, "/api/group-probe/run"},
		{http.MethodGet, "/api/group-probe/results"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, nil)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assert.Equal(t, http.StatusUnauthorized, response.Code, "%s %s", test.method, test.path)
		assert.NotContains(t, response.Body.String(), `"success":true`)
	}
}
