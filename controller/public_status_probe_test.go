package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const publicStatusProbeTestTargets = `[
  {"key":"public-codex","group":"codex","display_name":"Codex","model":"gpt-5.5","protocol":"openai_responses","channel_id":42,"key_index":3},
  {"key":"public-gemini","group":"gemini","display_name":"Gemini","model":"gemini-2.5-pro","protocol":"gemini_generate_content","channel_id":99}
]`

const publicStatusProbeTestSingleTarget = `[
  {"key":"public-codex","group":"codex","display_name":"Codex","model":"gpt-5.5","protocol":"openai_responses","channel_id":42,"key_index":3}
]`

func resetPublicStatusProbeControllerTestState(t *testing.T) {
	t.Helper()
	previousNow := publicStatusProbeNow
	previousLatestResults := publicStatusProbeLatestResults
	publicStatusProbeCacheMu.Lock()
	previousCache := publicStatusProbeCache
	publicStatusProbeCache = publicStatusProbeCacheEntry{}
	publicStatusProbeCacheMu.Unlock()
	t.Cleanup(func() {
		publicStatusProbeNow = previousNow
		publicStatusProbeLatestResults = previousLatestResults
		publicStatusProbeCacheMu.Lock()
		publicStatusProbeCache = previousCache
		publicStatusProbeCacheMu.Unlock()
	})
	gin.SetMode(gin.TestMode)
}

func configurePublicStatusProbeControllerTest(t *testing.T, enabled, targets string) {
	t.Helper()
	t.Setenv("PUBLIC_STATUS_PROBE_ENABLED", enabled)
	t.Setenv("PUBLIC_STATUS_PROBE_TARGETS", targets)
	t.Setenv("PUBLIC_STATUS_PROBE_INTERVAL_SECONDS", "60")
}

func performPublicStatusProbeRequest(ifNoneMatch string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/status/probes", nil)
	if ifNoneMatch != "" {
		context.Request.Header.Set("If-None-Match", ifNoneMatch)
	}
	GetPublicStatusProbes(context)
	return recorder
}

func int64Pointer(value int64) *int64 {
	return &value
}

func TestPublicStatusProbeExactContractAndPrivateFieldsExcluded(t *testing.T) {
	resetPublicStatusProbeControllerTestState(t)
	configurePublicStatusProbeControllerTest(t, "true", publicStatusProbeTestTargets)
	publicStatusProbeNow = func() time.Time { return time.Unix(1_786_852_274, 0) }
	publicStatusProbeLatestResults = func(targetKey string, limit int) ([]model.PublicStatusProbeResult, error) {
		assert.Equal(t, model.MaxPublicStatusProbeHistory, limit)
		switch targetKey {
		case "public-codex":
			return []model.PublicStatusProbeResult{
				{ID: 1, TargetKey: targetKey, GroupName: "private-db-group", DisplayName: "private-db-name", ModelName: "private-db-model", ChannelID: 42, SlotStartedAt: 1_786_852_100, CheckedAt: 1_786_852_114, State: model.PublicStatusProbeStateOperational, PingLatencyMS: int64Pointer(257), ChatLatencyMS: int64Pointer(5_061)},
				{ID: 2, TargetKey: targetKey, ChannelID: 42, SlotStartedAt: 1_786_852_160, CheckedAt: 1_786_852_174, State: model.PublicStatusProbeStateDegraded, PingLatencyMS: int64Pointer(301), ChatLatencyMS: int64Pointer(6_100), ErrorCode: "provider_rejected"},
				{ID: 3, TargetKey: targetKey, ChannelID: 42, SlotStartedAt: 1_786_852_220, CheckedAt: 1_786_852_234, State: model.PublicStatusProbeStateFailed, ErrorCode: "timeout"},
			}, nil
		case "public-gemini":
			return nil, nil
		default:
			t.Fatalf("unexpected target key %q", targetKey)
			return nil, nil
		}
	}

	recorder := performPublicStatusProbeRequest("")

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "public, max-age=15", recorder.Header().Get("Cache-Control"))
	assert.Regexp(t, `^W/"[0-9a-f]{64}"$`, recorder.Header().Get("ETag"))
	assert.JSONEq(t, `{
      "success":true,
      "data":{
        "generated_at":1786852274,
        "interval_seconds":60,
        "targets":[
          {
            "key":"public-codex",
            "group":"codex",
            "display_name":"Codex",
            "model":"gpt-5.5",
            "state":"failed",
            "availability":0.6666666666666666,
            "ping_latency_ms":null,
            "chat_latency_ms":null,
            "latest_checked_at":1786852234,
            "next_check_at":1786852320,
            "history":[
              {"checked_at":1786852114,"state":"operational","ping_latency_ms":257,"chat_latency_ms":5061,"error_code":null},
              {"checked_at":1786852174,"state":"degraded","ping_latency_ms":301,"chat_latency_ms":6100,"error_code":"provider_rejected"},
              {"checked_at":1786852234,"state":"failed","ping_latency_ms":null,"chat_latency_ms":null,"error_code":"timeout"}
            ]
          },
          {
            "key":"public-gemini",
            "group":"gemini",
            "display_name":"Gemini",
            "model":"gemini-2.5-pro",
            "state":"unknown",
            "availability":null,
            "ping_latency_ms":null,
            "chat_latency_ms":null,
            "latest_checked_at":null,
            "next_check_at":1786852320,
            "history":[]
          }
        ]
      }
    }`, recorder.Body.String())
	for _, privateMarker := range []string{
		`"channel_id"`, `"key_index"`, `"base_url"`, `"api_key"`, `"target_key"`,
		`"slot_started_at"`, `"id"`, "private-db-group", "private-db-name", "private-db-model",
	} {
		assert.NotContains(t, recorder.Body.String(), privateMarker)
	}
}

func TestPublicStatusProbeCacheExpiresAfterFifteenSeconds(t *testing.T) {
	resetPublicStatusProbeControllerTestState(t)
	configurePublicStatusProbeControllerTest(t, "true", publicStatusProbeTestSingleTarget)
	now := time.Unix(1_786_852_274, 0)
	publicStatusProbeNow = func() time.Time { return now }
	queryCount := 0
	publicStatusProbeLatestResults = func(string, int) ([]model.PublicStatusProbeResult, error) {
		queryCount++
		return nil, nil
	}

	first := performPublicStatusProbeRequest("")
	second := performPublicStatusProbeRequest("")
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, http.StatusOK, second.Code)
	assert.Equal(t, 1, queryCount)
	assert.Equal(t, first.Body.String(), second.Body.String())

	now = now.Add(publicStatusProbeCacheMaxAge)
	third := performPublicStatusProbeRequest("")
	require.Equal(t, http.StatusOK, third.Code)
	assert.Equal(t, 2, queryCount)
}

func TestPublicStatusProbeETagReturnsNotModified(t *testing.T) {
	resetPublicStatusProbeControllerTestState(t)
	configurePublicStatusProbeControllerTest(t, "true", publicStatusProbeTestSingleTarget)
	publicStatusProbeNow = func() time.Time { return time.Unix(1_786_852_274, 0) }
	queryCount := 0
	publicStatusProbeLatestResults = func(string, int) ([]model.PublicStatusProbeResult, error) {
		queryCount++
		return nil, nil
	}

	first := performPublicStatusProbeRequest("")
	etag := first.Header().Get("ETag")
	require.NotEmpty(t, etag)
	second := performPublicStatusProbeRequest(`"different", ` + etag)

	assert.Equal(t, http.StatusNotModified, second.Code)
	assert.Empty(t, second.Body.String())
	assert.Equal(t, etag, second.Header().Get("ETag"))
	assert.Equal(t, "public, max-age=15", second.Header().Get("Cache-Control"))
	assert.Equal(t, 1, queryCount)

	strongETag := strings.TrimPrefix(etag, "W/")
	third := performPublicStatusProbeRequest(strongETag)
	assert.Equal(t, http.StatusNotModified, third.Code)
	assert.Empty(t, third.Body.String())
}

func TestPublicStatusProbeDisabledOrEmptyReturnsEmptyTargets(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		enabled string
		targets string
	}{
		{name: "disabled", enabled: "false", targets: publicStatusProbeTestTargets},
		{name: "empty", enabled: "true", targets: "[]"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetPublicStatusProbeControllerTestState(t)
			configurePublicStatusProbeControllerTest(t, testCase.enabled, testCase.targets)
			publicStatusProbeNow = func() time.Time { return time.Unix(1_786_852_274, 0) }
			publicStatusProbeLatestResults = func(string, int) ([]model.PublicStatusProbeResult, error) {
				t.Fatal("disabled or empty configuration must not query probe results")
				return nil, nil
			}

			recorder := performPublicStatusProbeRequest("")

			require.Equal(t, http.StatusOK, recorder.Code)
			assert.JSONEq(t, `{"success":true,"data":{"generated_at":1786852274,"interval_seconds":60,"targets":[]}}`, recorder.Body.String())
		})
	}
}

func TestPublicStatusProbeInvalidConfigurationIsPubliclyDisabled(t *testing.T) {
	resetPublicStatusProbeControllerTestState(t)
	configurePublicStatusProbeControllerTest(t, "true", `[{"key":"private-invalid-marker"}]`)
	publicStatusProbeNow = func() time.Time { return time.Unix(1_786_852_274, 0) }
	publicStatusProbeLatestResults = func(string, int) ([]model.PublicStatusProbeResult, error) {
		t.Fatal("invalid configuration must not query probe results")
		return nil, nil
	}

	recorder := performPublicStatusProbeRequest("")

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"success":true,"data":{"generated_at":1786852274,"interval_seconds":60,"targets":[]}}`, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "private-invalid-marker")
}

func TestPublicStatusProbeDatabaseErrorIsPrivate(t *testing.T) {
	resetPublicStatusProbeControllerTestState(t)
	configurePublicStatusProbeControllerTest(t, "true", publicStatusProbeTestSingleTarget)
	publicStatusProbeNow = func() time.Time { return time.Unix(1_786_852_274, 0) }
	publicStatusProbeLatestResults = func(string, int) ([]model.PublicStatusProbeResult, error) {
		return nil, errors.New("mysql password=secret-marker host=private-db.internal stack=/srv/new-api")
	}

	recorder := performPublicStatusProbeRequest("")

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.Empty(t, recorder.Header().Get("ETag"))
	assert.JSONEq(t, `{"success":false,"message":"failed to load public status probes"}`, recorder.Body.String())
	for _, privateMarker := range []string{"secret-marker", "private-db.internal", "/srv/new-api", "password", "mysql"} {
		assert.NotContains(t, recorder.Body.String(), privateMarker)
	}
}

func TestPublicStatusProbeDatabaseErrorsHaveShortNegativeCache(t *testing.T) {
	resetPublicStatusProbeControllerTestState(t)
	configurePublicStatusProbeControllerTest(t, "true", publicStatusProbeTestSingleTarget)
	now := time.Unix(1_786_852_274, 0)
	publicStatusProbeNow = func() time.Time { return now }
	queryCount := 0
	publicStatusProbeLatestResults = func(string, int) ([]model.PublicStatusProbeResult, error) {
		queryCount++
		return nil, errors.New("private database failure")
	}

	assert.Equal(t, http.StatusInternalServerError, performPublicStatusProbeRequest("").Code)
	assert.Equal(t, http.StatusInternalServerError, performPublicStatusProbeRequest("").Code)
	assert.Equal(t, 1, queryCount)

	now = now.Add(publicStatusProbeFailureBackoff)
	assert.Equal(t, http.StatusInternalServerError, performPublicStatusProbeRequest("").Code)
	assert.Equal(t, 2, queryCount)
}

func TestPublicStatusProbeErrorCodeUsesPublicAllowlist(t *testing.T) {
	for _, code := range []string{
		"unsupported_provider",
		"timeout",
		"network_error",
		"provider_rejected",
		"empty_response",
		"validation_failed",
		"invalid_target",
		"response_too_large",
	} {
		require.NotNil(t, publicStatusProbeErrorCode(code))
		assert.Equal(t, code, *publicStatusProbeErrorCode(code))
	}
	assert.Nil(t, publicStatusProbeErrorCode(""))
	assert.Equal(t, "network_error", *publicStatusProbeErrorCode("api_key_secret_marker"))
}

func TestPublicStatusProbeTargetDefensivelyKeepsLatestSixtyResults(t *testing.T) {
	results := make([]model.PublicStatusProbeResult, 65)
	for index := range results {
		results[index] = model.PublicStatusProbeResult{
			CheckedAt: int64(index),
			State:     model.PublicStatusProbeStateOperational,
		}
	}

	target := publicStatusProbeTarget(public_status_probe_setting.Target{}, results, 120)

	require.Len(t, target.History, model.MaxPublicStatusProbeHistory)
	assert.Equal(t, int64(5), target.History[0].CheckedAt)
	assert.Equal(t, int64(64), target.History[len(target.History)-1].CheckedAt)
}
