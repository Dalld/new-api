package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	} {
		_, exists := routes[route]
		assert.True(t, exists, "missing route %s", route)
	}
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
