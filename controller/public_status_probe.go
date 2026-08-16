package controller

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/gin-gonic/gin"
)

const (
	publicStatusProbeCacheMaxAge    = 15 * time.Second
	publicStatusProbeFailureBackoff = 2 * time.Second
)

var errPublicStatusProbeCachedFailure = errors.New("public status probe data temporarily unavailable")

type publicStatusProbePointDTO struct {
	CheckedAt     int64   `json:"checked_at"`
	State         string  `json:"state"`
	PingLatencyMS *int64  `json:"ping_latency_ms"`
	ChatLatencyMS *int64  `json:"chat_latency_ms"`
	ErrorCode     *string `json:"error_code"`
}

type publicStatusProbeTargetDTO struct {
	Key             string                      `json:"key"`
	Group           string                      `json:"group"`
	DisplayName     string                      `json:"display_name"`
	Model           string                      `json:"model"`
	State           string                      `json:"state"`
	Availability    *float64                    `json:"availability"`
	PingLatencyMS   *int64                      `json:"ping_latency_ms"`
	ChatLatencyMS   *int64                      `json:"chat_latency_ms"`
	LatestCheckedAt *int64                      `json:"latest_checked_at"`
	NextCheckAt     int64                       `json:"next_check_at"`
	History         []publicStatusProbePointDTO `json:"history"`
}

type publicStatusProbeDataDTO struct {
	GeneratedAt     int64                        `json:"generated_at"`
	IntervalSeconds int64                        `json:"interval_seconds"`
	Targets         []publicStatusProbeTargetDTO `json:"targets"`
}

type publicStatusProbeResponseDTO struct {
	Success bool                     `json:"success"`
	Data    publicStatusProbeDataDTO `json:"data"`
}

type publicStatusProbeErrorDTO struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type publicStatusProbeCacheEntry struct {
	body         []byte
	etag         string
	expiresAt    time.Time
	failureUntil time.Time
}

var (
	publicStatusProbeCacheMu sync.Mutex
	publicStatusProbeCache   publicStatusProbeCacheEntry

	publicStatusProbeNow           = time.Now
	publicStatusProbeLatestResults = model.GetLatestPublicStatusProbeResults
)

func GetPublicStatusProbes(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=15")

	body, etag, err := getPublicStatusProbeResponse()
	if err != nil {
		writePublicStatusProbeError(c)
		return
	}

	c.Header("ETag", etag)
	if publicStatusProbeETagMatches(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		c.Writer.WriteHeaderNow()
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

func getPublicStatusProbeResponse() ([]byte, string, error) {
	publicStatusProbeCacheMu.Lock()
	defer publicStatusProbeCacheMu.Unlock()

	now := publicStatusProbeNow().UTC()
	if publicStatusProbeCache.body != nil && now.Before(publicStatusProbeCache.expiresAt) {
		return publicStatusProbeCache.body, publicStatusProbeCache.etag, nil
	}
	if now.Before(publicStatusProbeCache.failureUntil) {
		return nil, "", errPublicStatusProbeCachedFailure
	}

	response, err := buildPublicStatusProbeResponse(now)
	if err != nil {
		publicStatusProbeCache.failureUntil = now.Add(publicStatusProbeFailureBackoff)
		return nil, "", err
	}
	body, err := common.Marshal(response)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(body)
	etag := fmt.Sprintf("W/\"%x\"", digest)
	publicStatusProbeCache = publicStatusProbeCacheEntry{
		body:         body,
		etag:         etag,
		expiresAt:    now.Add(publicStatusProbeCacheMaxAge),
		failureUntil: time.Time{},
	}
	return body, etag, nil
}

func buildPublicStatusProbeResponse(now time.Time) (publicStatusProbeResponseDTO, error) {
	probeSetting, err := public_status_probe_setting.Load()
	if err != nil {
		return publicStatusProbeResponseDTO{
			Success: true,
			Data: publicStatusProbeDataDTO{
				GeneratedAt:     now.Unix(),
				IntervalSeconds: 60,
				Targets:         make([]publicStatusProbeTargetDTO, 0),
			},
		}, nil
	}

	intervalSeconds := int64(probeSetting.Interval / time.Second)
	data := publicStatusProbeDataDTO{
		GeneratedAt:     now.Unix(),
		IntervalSeconds: intervalSeconds,
		Targets:         make([]publicStatusProbeTargetDTO, 0),
	}
	if !probeSetting.Enabled || len(probeSetting.Targets) == 0 {
		return publicStatusProbeResponseDTO{Success: true, Data: data}, nil
	}

	nextCheckAt := now.Truncate(probeSetting.Interval).Add(probeSetting.Interval).Unix()
	data.Targets = make([]publicStatusProbeTargetDTO, 0, len(probeSetting.Targets))
	for _, configuredTarget := range probeSetting.Targets {
		results, queryErr := publicStatusProbeLatestResults(configuredTarget.Key, model.MaxPublicStatusProbeHistory)
		if queryErr != nil {
			return publicStatusProbeResponseDTO{}, queryErr
		}
		data.Targets = append(data.Targets, publicStatusProbeTarget(configuredTarget, results, nextCheckAt))
	}

	return publicStatusProbeResponseDTO{Success: true, Data: data}, nil
}

func publicStatusProbeTarget(configuredTarget public_status_probe_setting.Target, results []model.PublicStatusProbeResult, nextCheckAt int64) publicStatusProbeTargetDTO {
	if len(results) > model.MaxPublicStatusProbeHistory {
		results = results[len(results)-model.MaxPublicStatusProbeHistory:]
	}
	target := publicStatusProbeTargetDTO{
		Key:         configuredTarget.Key,
		Group:       configuredTarget.Group,
		DisplayName: configuredTarget.DisplayName,
		Model:       configuredTarget.Model,
		State:       string(model.PublicStatusProbeStateUnknown),
		NextCheckAt: nextCheckAt,
		History:     make([]publicStatusProbePointDTO, 0, len(results)),
	}

	successful := 0
	for _, result := range results {
		if result.State == model.PublicStatusProbeStateOperational || result.State == model.PublicStatusProbeStateDegraded {
			successful++
		}
		target.History = append(target.History, publicStatusProbePointDTO{
			CheckedAt:     result.CheckedAt,
			State:         string(result.State),
			PingLatencyMS: result.PingLatencyMS,
			ChatLatencyMS: result.ChatLatencyMS,
			ErrorCode:     publicStatusProbeErrorCode(result.ErrorCode),
		})
	}

	if len(results) == 0 {
		return target
	}
	availability := float64(successful) / float64(len(results))
	latest := results[len(results)-1]
	target.State = string(latest.State)
	target.Availability = &availability
	target.PingLatencyMS = latest.PingLatencyMS
	target.ChatLatencyMS = latest.ChatLatencyMS
	target.LatestCheckedAt = &latest.CheckedAt
	return target
}

func publicStatusProbeErrorCode(code string) *string {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil
	}
	value := "network_error"
	switch code {
	case "unsupported_provider",
		"timeout",
		"network_error",
		"provider_rejected",
		"empty_response",
		"validation_failed",
		"invalid_target",
		"response_too_large":
		value = code
	}
	return &value
}

func publicStatusProbeETagMatches(headerValue, etag string) bool {
	normalizedETag := strings.TrimPrefix(etag, "W/")
	for _, candidate := range strings.Split(headerValue, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == normalizedETag {
			return true
		}
	}
	return false
}

func writePublicStatusProbeError(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	body, err := common.Marshal(publicStatusProbeErrorDTO{
		Success: false,
		Message: "failed to load public status probes",
	})
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusInternalServerError, "application/json; charset=utf-8", body)
}
