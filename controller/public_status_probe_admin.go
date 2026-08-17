package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	probeservice "github.com/QuantumNous/new-api/service/public_status_probe"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const publicStatusProbeAdminMaxRequestBytes = 64 * 1024
const publicStatusProbeAdminMaxTargetKeyRunes = 96

var (
	errPublicStatusProbeAdminBadRequest = errors.New("invalid public status probe request")
	errPublicStatusProbeTargetMissing   = errors.New("public status probe target not found")
)

type publicStatusProbeAdminConfigUpdateRequest struct {
	Version            int64 `json:"version"`
	Enabled            bool  `json:"enabled"`
	PingTimeoutSeconds int   `json:"ping_timeout_seconds"`
	ChatTimeoutSeconds int   `json:"chat_timeout_seconds"`
	DegradedLatencyMS  int   `json:"degraded_latency_ms"`
	Concurrency        int   `json:"concurrency"`
	RetentionDays      int   `json:"retention_days"`
}

type publicStatusProbeAdminTargetRequest struct {
	Version     int64                             `json:"version"`
	Enabled     bool                              `json:"enabled"`
	Key         string                            `json:"key"`
	Group       string                            `json:"group"`
	DisplayName string                            `json:"display_name"`
	Model       string                            `json:"model"`
	Protocol    publicstatusprobesetting.Protocol `json:"protocol"`
	ChannelID   int                               `json:"channel_id"`
	KeyIndex    int                               `json:"key_index"`
}

type publicStatusProbeAdminChannelDTO struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Type       int    `json:"type"`
	Status     int    `json:"status"`
	Models     string `json:"models"`
	IsMultiKey bool   `json:"is_multi_key"`
	KeyCount   int    `json:"key_count"`
}

type publicStatusProbeAdminTargetDTO struct {
	Enabled     bool                              `json:"enabled"`
	Key         string                            `json:"key"`
	Group       string                            `json:"group"`
	DisplayName string                            `json:"display_name"`
	Model       string                            `json:"model"`
	Protocol    publicstatusprobesetting.Protocol `json:"protocol"`
	ChannelID   int                               `json:"channel_id"`
	KeyIndex    int                               `json:"key_index"`
	Channel     publicStatusProbeAdminChannelDTO  `json:"channel"`
}

type publicStatusProbeAdminConfigDTO struct {
	Version            int64                              `json:"version"`
	Enabled            bool                               `json:"enabled"`
	PingTimeoutSeconds int                                `json:"ping_timeout_seconds"`
	ChatTimeoutSeconds int                                `json:"chat_timeout_seconds"`
	DegradedLatencyMS  int                                `json:"degraded_latency_ms"`
	Concurrency        int                                `json:"concurrency"`
	RetentionDays      int                                `json:"retention_days"`
	Targets            []publicStatusProbeAdminTargetDTO  `json:"targets"`
	Channels           []publicStatusProbeAdminChannelDTO `json:"channels"`
}

type publicStatusProbeAdminResponse struct {
	Success bool                            `json:"success"`
	Message string                          `json:"message"`
	Data    publicStatusProbeAdminConfigDTO `json:"data"`
}

type publicStatusProbeAdminConflictResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Version int64 `json:"version"`
	} `json:"data"`
}

func GetPublicStatusProbeConfig(c *gin.Context) {
	response, err := buildPublicStatusProbeAdminResponse(publicstatusprobesetting.CurrentDocument())
	if err != nil {
		writePublicStatusProbeAdminError(c, http.StatusInternalServerError, "failed to load public status probe config")
		return
	}
	common.ApiSuccess(c, response.Data)
}

func UpdatePublicStatusProbeConfig(c *gin.Context) {
	var request publicStatusProbeAdminConfigUpdateRequest
	if err := decodePublicStatusProbeAdminRequest(c, &request); err != nil {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.config_update", "", request.Version)
		writePublicStatusProbeAdminError(c, http.StatusBadRequest, "invalid public status probe configuration")
		return
	}

	updated, err := model.CompareAndSwapPublicStatusProbeConfig(request.Version, func(next *publicstatusprobesetting.Document) error {
		next.Enabled = request.Enabled
		next.PingTimeoutSeconds = request.PingTimeoutSeconds
		next.ChatTimeoutSeconds = request.ChatTimeoutSeconds
		next.DegradedLatencyMS = request.DegradedLatencyMS
		next.Concurrency = request.Concurrency
		next.RetentionDays = request.RetentionDays

		normalized, validateErr := publicstatusprobesetting.ValidateAndNormalizeDocument(*next)
		if validateErr != nil {
			return validateErr
		}
		*next = normalized
		return nil
	})
	if err != nil {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.config_update", "", request.Version)
		writePublicStatusProbeAdminMutationError(c, err)
		return
	}

	recordManageAudit(c, "public_status_probe.config_update", map[string]interface{}{
		"version": updated.Version,
	})
	response, err := buildPublicStatusProbeAdminResponse(updated)
	if err != nil {
		writePublicStatusProbeAdminError(c, http.StatusInternalServerError, "failed to load public status probe config")
		return
	}
	common.ApiSuccess(c, response.Data)
}

func CreatePublicStatusProbeTarget(c *gin.Context) {
	var request publicStatusProbeAdminTargetRequest
	if err := decodePublicStatusProbeAdminRequest(c, &request); err != nil {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.target_create", "", request.Version)
		writePublicStatusProbeAdminError(c, http.StatusBadRequest, "invalid public status probe target")
		return
	}

	target := publicstatusprobesetting.Target{
		Enabled:     request.Enabled,
		Key:         "probe-" + uuid.NewString(),
		Group:       strings.TrimSpace(request.Group),
		DisplayName: strings.TrimSpace(request.DisplayName),
		Model:       strings.TrimSpace(request.Model),
		Protocol:    request.Protocol,
		ChannelID:   request.ChannelID,
		KeyIndex:    request.KeyIndex,
	}

	updated, err := model.CompareAndSwapPublicStatusProbeConfig(request.Version, func(next *publicstatusprobesetting.Document) error {
		if err := validatePublicStatusProbeTarget(c, target); err != nil {
			return err
		}
		candidate := *next
		candidate.Version = request.Version
		candidate.Targets = append(append([]publicstatusprobesetting.Target(nil), next.Targets...), target)
		normalized, validateErr := publicstatusprobesetting.ValidateAndNormalizeDocument(candidate)
		if validateErr != nil {
			return validateErr
		}
		*next = normalized
		return nil
	})
	if err != nil {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.target_create", "", request.Version)
		writePublicStatusProbeAdminMutationError(c, err)
		return
	}

	recordManageAudit(c, "public_status_probe.target_create", map[string]interface{}{
		"target_key": target.Key,
		"version":    updated.Version,
	})
	response, err := buildPublicStatusProbeAdminResponse(updated)
	if err != nil {
		writePublicStatusProbeAdminError(c, http.StatusInternalServerError, "failed to load public status probe config")
		return
	}
	common.ApiSuccess(c, response.Data)
}

func UpdatePublicStatusProbeTarget(c *gin.Context) {
	pathKey := strings.TrimSpace(c.Param("key"))
	if pathKey == "" {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.target_update", "", 0)
		writePublicStatusProbeAdminError(c, http.StatusNotFound, "public status probe target not found")
		return
	}

	var request publicStatusProbeAdminTargetRequest
	if err := decodePublicStatusProbeAdminRequest(c, &request); err != nil {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.target_update", pathKey, request.Version)
		writePublicStatusProbeAdminError(c, http.StatusBadRequest, "invalid public status probe target")
		return
	}

	target := publicstatusprobesetting.Target{
		Enabled:     request.Enabled,
		Key:         pathKey,
		Group:       strings.TrimSpace(request.Group),
		DisplayName: strings.TrimSpace(request.DisplayName),
		Model:       strings.TrimSpace(request.Model),
		Protocol:    request.Protocol,
		ChannelID:   request.ChannelID,
		KeyIndex:    request.KeyIndex,
	}

	updated, err := model.CompareAndSwapPublicStatusProbeConfig(request.Version, func(next *publicstatusprobesetting.Document) error {
		index := findPublicStatusProbeTargetIndex(next.Targets, pathKey)
		if index < 0 {
			return errPublicStatusProbeTargetMissing
		}
		if target.Enabled {
			if err := validatePublicStatusProbeTarget(c, target); err != nil {
				return err
			}
		}
		candidate := *next
		candidate.Version = request.Version
		candidate.Targets = append([]publicstatusprobesetting.Target(nil), next.Targets...)
		target.Key = pathKey
		candidate.Targets[index] = target
		normalized, validateErr := publicstatusprobesetting.ValidateAndNormalizeDocument(candidate)
		if validateErr != nil {
			return validateErr
		}
		*next = normalized
		return nil
	})
	if err != nil {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.target_update", pathKey, request.Version)
		writePublicStatusProbeAdminMutationError(c, err)
		return
	}

	recordManageAudit(c, "public_status_probe.target_update", map[string]interface{}{
		"target_key": pathKey,
		"version":    updated.Version,
	})
	response, err := buildPublicStatusProbeAdminResponse(updated)
	if err != nil {
		writePublicStatusProbeAdminError(c, http.StatusInternalServerError, "failed to load public status probe config")
		return
	}
	common.ApiSuccess(c, response.Data)
}

func DeletePublicStatusProbeTarget(c *gin.Context) {
	pathKey := strings.TrimSpace(c.Param("key"))
	if pathKey == "" {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.target_delete", "", 0)
		writePublicStatusProbeAdminError(c, http.StatusNotFound, "public status probe target not found")
		return
	}

	versionRaw := strings.TrimSpace(c.Query("version"))
	version, err := strconv.ParseInt(versionRaw, 10, 64)
	if err != nil || version <= 0 {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.target_delete", pathKey, 0)
		writePublicStatusProbeAdminError(c, http.StatusBadRequest, "invalid public status probe version")
		return
	}

	updated, err := model.CompareAndSwapPublicStatusProbeConfig(version, func(next *publicstatusprobesetting.Document) error {
		index := findPublicStatusProbeTargetIndex(next.Targets, pathKey)
		if index < 0 {
			return errPublicStatusProbeTargetMissing
		}
		candidate := *next
		candidate.Version = version
		candidate.Targets = append([]publicstatusprobesetting.Target(nil), next.Targets[:index]...)
		candidate.Targets = append(candidate.Targets, next.Targets[index+1:]...)
		normalized, validateErr := publicstatusprobesetting.ValidateAndNormalizeDocument(candidate)
		if validateErr != nil {
			return validateErr
		}
		*next = normalized
		return nil
	})
	if err != nil {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.target_delete", pathKey, version)
		writePublicStatusProbeAdminMutationError(c, err)
		return
	}

	recordManageAudit(c, "public_status_probe.target_delete", map[string]interface{}{
		"target_key": pathKey,
		"version":    updated.Version,
	})
	response, err := buildPublicStatusProbeAdminResponse(updated)
	if err != nil {
		writePublicStatusProbeAdminError(c, http.StatusInternalServerError, "failed to load public status probe config")
		return
	}
	common.ApiSuccess(c, response.Data)
}

func decodePublicStatusProbeAdminRequest(c *gin.Context, out any) error {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return errPublicStatusProbeAdminBadRequest
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, publicStatusProbeAdminMaxRequestBytes+1))
	if err != nil {
		return errPublicStatusProbeAdminBadRequest
	}
	if len(raw) == 0 || len(raw) > publicStatusProbeAdminMaxRequestBytes {
		return errPublicStatusProbeAdminBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return errPublicStatusProbeAdminBadRequest
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errPublicStatusProbeAdminBadRequest
	}
	return nil
}

func validatePublicStatusProbeTarget(c *gin.Context, target publicstatusprobesetting.Target) error {
	loader := probeservice.NewDBTargetLoader(model.DB)
	if err := loader.ValidateTarget(c.Request.Context(), target); err != nil {
		return err
	}
	return nil
}

func buildPublicStatusProbeAdminResponse(document publicstatusprobesetting.Document) (publicStatusProbeAdminResponse, error) {
	var channels []model.Channel
	if err := model.DB.
		Select("id", "name", "type", "status", "models", "key", "channel_info").
		Order("id ASC").
		Find(&channels).Error; err != nil {
		return publicStatusProbeAdminResponse{}, err
	}
	channelByID := make(map[int]publicStatusProbeAdminChannelDTO, len(channels))
	channelDTOs := make([]publicStatusProbeAdminChannelDTO, 0, len(channels))
	for _, channel := range channels {
		channelDTO := buildPublicStatusProbeAdminChannelDTO(channel)
		channelDTOs = append(channelDTOs, channelDTO)
		channelByID[channel.Id] = channelDTO
	}

	response := publicStatusProbeAdminResponse{
		Success: true,
		Data: publicStatusProbeAdminConfigDTO{
			Version:            document.Version,
			Enabled:            document.Enabled,
			PingTimeoutSeconds: document.PingTimeoutSeconds,
			ChatTimeoutSeconds: document.ChatTimeoutSeconds,
			DegradedLatencyMS:  document.DegradedLatencyMS,
			Concurrency:        document.Concurrency,
			RetentionDays:      document.RetentionDays,
			Targets:            make([]publicStatusProbeAdminTargetDTO, 0, len(document.Targets)),
			Channels:           channelDTOs,
		},
	}

	for _, target := range document.Targets {
		channelDTO, exists := channelByID[target.ChannelID]
		if !exists {
			channelDTO = publicStatusProbeAdminChannelDTO{
				ID:     target.ChannelID,
				Status: common.ChannelStatusManuallyDisabled,
			}
		}
		response.Data.Targets = append(response.Data.Targets, publicStatusProbeAdminTargetDTO{
			Enabled:     target.Enabled,
			Key:         target.Key,
			Group:       target.Group,
			DisplayName: target.DisplayName,
			Model:       target.Model,
			Protocol:    target.Protocol,
			ChannelID:   target.ChannelID,
			KeyIndex:    target.KeyIndex,
			Channel:     channelDTO,
		})
	}

	return response, nil
}

func buildPublicStatusProbeAdminChannelDTO(channel model.Channel) publicStatusProbeAdminChannelDTO {
	keyCount := len(channel.GetKeys())
	if channel.ChannelInfo.IsMultiKey && channel.ChannelInfo.MultiKeySize > 0 {
		keyCount = channel.ChannelInfo.MultiKeySize
	}
	return publicStatusProbeAdminChannelDTO{
		ID:         channel.Id,
		Name:       channel.Name,
		Type:       channel.Type,
		Status:     channel.Status,
		Models:     channel.Models,
		IsMultiKey: channel.ChannelInfo.IsMultiKey,
		KeyCount:   keyCount,
	}
}

func writePublicStatusProbeAdminMutationError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, errPublicStatusProbeTargetMissing), errors.Is(err, gorm.ErrRecordNotFound):
		writePublicStatusProbeAdminError(c, http.StatusNotFound, "public status probe target not found")
	case errors.Is(err, model.ErrPublicStatusProbeConfigConflict):
		writePublicStatusProbeAdminConflict(c)
	default:
		switch probeservice.ErrorCodeOf(err) {
		case probeservice.ErrorInvalidTarget, probeservice.ErrorUnsupportedProvider, probeservice.ErrorValidationFailed, probeservice.ErrorResponseTooLarge:
			writePublicStatusProbeAdminError(c, http.StatusBadRequest, "invalid public status probe target")
		case probeservice.ErrorNetwork, probeservice.ErrorTimeout, probeservice.ErrorEmptyResponse, probeservice.ErrorProviderRejected:
			writePublicStatusProbeAdminError(c, http.StatusBadRequest, "invalid public status probe target")
		default:
			if strings.Contains(strings.ToLower(err.Error()), "record not found") {
				writePublicStatusProbeAdminError(c, http.StatusNotFound, "public status probe target not found")
				return
			}
			writePublicStatusProbeAdminError(c, http.StatusBadRequest, "invalid public status probe request")
		}
	}
}

func writePublicStatusProbeAdminConflict(c *gin.Context) {
	version := publicstatusprobesetting.CurrentDocument().Version
	if current, err := model.GetPublicStatusProbeConfig(); err == nil {
		version = current.Version
	}
	response := publicStatusProbeAdminConflictResponse{
		Success: false,
		Message: "public status probe configuration conflict",
	}
	response.Data.Version = version
	c.AbortWithStatusJSON(http.StatusConflict, response)
}

func recordPublicStatusProbeAdminRejectedAudit(c *gin.Context, action string, targetKey string, version int64) {
	params := map[string]interface{}{
		"success": false,
	}
	if version > 0 {
		params["version"] = version
	}
	targetKey = strings.TrimSpace(targetKey)
	if targetKey != "" && utf8.ValidString(targetKey) && utf8.RuneCountInString(targetKey) <= publicStatusProbeAdminMaxTargetKeyRunes {
		params["target_key"] = targetKey
	}
	recordManageAudit(c, action, params)
}

func writePublicStatusProbeAdminError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, publicStatusProbeAdminResponse{
		Success: false,
		Message: message,
	})
}

func findPublicStatusProbeTargetIndex(targets []publicstatusprobesetting.Target, key string) int {
	for index, target := range targets {
		if target.Key == key {
			return index
		}
	}
	return -1
}
