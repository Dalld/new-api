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
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	probeservice "github.com/QuantumNous/new-api/service/public_status_probe"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const publicStatusProbeAdminMaxRequestBytes = 64 * 1024
const publicStatusProbeAdminMaxTargetKeyRunes = 96
const publicStatusProbeAdminMaxTargetGroupRunes = 64
const publicStatusProbeAdminMaxTargetDisplayNameRunes = 128
const publicStatusProbeAdminMaxTargetModelRunes = 128
const publicStatusProbeAdminFieldErrorInvalid = "invalid"

var (
	errPublicStatusProbeAdminBadRequest = errors.New("invalid public status probe request")
	errPublicStatusProbeTargetMissing   = errors.New("public status probe target not found")
	errPublicStatusProbeChannelLoad     = errors.New("failed to load public status probe channels")
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

type publicStatusProbeAdminFieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

type publicStatusProbeAdminValidationError struct {
	Message     string
	FieldErrors []publicStatusProbeAdminFieldError
}

func (e *publicStatusProbeAdminValidationError) Error() string {
	return e.Message
}

type publicStatusProbeAdminValidationResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		FieldErrors []publicStatusProbeAdminFieldError `json:"field_errors"`
	} `json:"data"`
}

type publicStatusProbeAdminChannelSnapshot struct {
	Channels        []publicStatusProbeAdminChannelDTO
	ChannelByID     map[int]publicStatusProbeAdminChannelDTO
	ChannelInfoByID map[int]model.ChannelInfo
}

func GetPublicStatusProbeConfig(c *gin.Context) {
	channels, err := loadPublicStatusProbeAdminChannelSnapshot(nil, nil)
	if err != nil {
		writePublicStatusProbeAdminError(c, http.StatusInternalServerError, "failed to load public status probe config")
		return
	}
	response := buildPublicStatusProbeAdminResponse(publicstatusprobesetting.CurrentDocument(), channels)
	common.ApiSuccess(c, response.Data)
}

func UpdatePublicStatusProbeConfig(c *gin.Context) {
	var request publicStatusProbeAdminConfigUpdateRequest
	if err := decodePublicStatusProbeAdminRequest(c, &request); err != nil {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.config_update", "", request.Version)
		writePublicStatusProbeAdminError(c, http.StatusBadRequest, "invalid public status probe configuration")
		return
	}
	if err := validatePublicStatusProbeAdminConfigRequest(request); err != nil {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.config_update", "", request.Version)
		writePublicStatusProbeAdminMutationError(c, err)
		return
	}
	var channels publicStatusProbeAdminChannelSnapshot
	updated, err := model.CompareAndSwapPublicStatusProbeConfigWithTransaction(request.Version, func(tx *gorm.DB, next *publicstatusprobesetting.Document) error {
		var loadErr error
		channels, loadErr = loadPublicStatusProbeAdminChannelSnapshot(tx, nil)
		if loadErr != nil {
			return errPublicStatusProbeChannelLoad
		}
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
	response := buildPublicStatusProbeAdminResponse(updated, channels)
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
	if request.Version <= 0 {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.target_create", "", request.Version)
		writePublicStatusProbeAdminValidationError(c, &publicStatusProbeAdminValidationError{
			"invalid public status probe target",
			[]publicStatusProbeAdminFieldError{{Field: "version", Code: publicStatusProbeAdminFieldErrorInvalid}},
		})
		return
	}
	var channels publicStatusProbeAdminChannelSnapshot
	updated, err := model.CompareAndSwapPublicStatusProbeConfigWithTransaction(request.Version, func(tx *gorm.DB, next *publicstatusprobesetting.Document) error {
		var loadErr error
		channels, loadErr = loadPublicStatusProbeAdminChannelSnapshot(tx, []int{target.ChannelID})
		if loadErr != nil {
			return errPublicStatusProbeChannelLoad
		}
		validationErr := validatePublicStatusProbeTarget(c, tx, target, channels, true)
		if validationErr != nil {
			return validationErr
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
	response := buildPublicStatusProbeAdminResponse(updated, channels)
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
	if request.Version <= 0 {
		recordPublicStatusProbeAdminRejectedAudit(c, "public_status_probe.target_update", pathKey, request.Version)
		writePublicStatusProbeAdminValidationError(c, &publicStatusProbeAdminValidationError{
			"invalid public status probe target",
			[]publicStatusProbeAdminFieldError{{Field: "version", Code: publicStatusProbeAdminFieldErrorInvalid}},
		})
		return
	}
	var channels publicStatusProbeAdminChannelSnapshot
	updated, err := model.CompareAndSwapPublicStatusProbeConfigWithTransaction(request.Version, func(tx *gorm.DB, next *publicstatusprobesetting.Document) error {
		index := findPublicStatusProbeTargetIndex(next.Targets, pathKey)
		if index < 0 {
			return errPublicStatusProbeTargetMissing
		}
		var loadErr error
		lockIDs := []int(nil)
		if target.Enabled {
			lockIDs = []int{target.ChannelID}
		}
		channels, loadErr = loadPublicStatusProbeAdminChannelSnapshot(tx, lockIDs)
		if loadErr != nil {
			return errPublicStatusProbeChannelLoad
		}
		validationErr := validatePublicStatusProbeTarget(c, tx, target, channels, target.Enabled)
		if validationErr != nil {
			return validationErr
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
	response := buildPublicStatusProbeAdminResponse(updated, channels)
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
	var channels publicStatusProbeAdminChannelSnapshot
	updated, err := model.CompareAndSwapPublicStatusProbeConfigWithTransaction(version, func(tx *gorm.DB, next *publicstatusprobesetting.Document) error {
		index := findPublicStatusProbeTargetIndex(next.Targets, pathKey)
		if index < 0 {
			return errPublicStatusProbeTargetMissing
		}
		lockIDs := []int{next.Targets[index].ChannelID}
		var loadErr error
		channels, loadErr = loadPublicStatusProbeAdminChannelSnapshot(tx, lockIDs)
		if loadErr != nil {
			return errPublicStatusProbeChannelLoad
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
	response := buildPublicStatusProbeAdminResponse(updated, channels)
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

func loadPublicStatusProbeAdminChannelSnapshot(db *gorm.DB, lockChannelIDs []int) (publicStatusProbeAdminChannelSnapshot, error) {
	channels, err := model.LoadPublicStatusProbeChannels(db, lockChannelIDs)
	if err != nil {
		return publicStatusProbeAdminChannelSnapshot{}, errPublicStatusProbeChannelLoad
	}
	channelByID := make(map[int]publicStatusProbeAdminChannelDTO, len(channels))
	channelInfoByID := make(map[int]model.ChannelInfo, len(channels))
	channelDTOs := make([]publicStatusProbeAdminChannelDTO, 0, len(channels))
	for _, channel := range channels {
		channelDTO := buildPublicStatusProbeAdminChannelDTO(channel)
		channelDTOs = append(channelDTOs, channelDTO)
		channelByID[channel.Id] = channelDTO
		channelInfoByID[channel.Id] = channel.ChannelInfo
	}
	return publicStatusProbeAdminChannelSnapshot{
		Channels:        channelDTOs,
		ChannelByID:     channelByID,
		ChannelInfoByID: channelInfoByID,
	}, nil
}

func buildPublicStatusProbeAdminResponse(document publicstatusprobesetting.Document, snapshot publicStatusProbeAdminChannelSnapshot) publicStatusProbeAdminResponse {
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
			Channels:           snapshot.Channels,
		},
	}

	for _, target := range document.Targets {
		channelDTO, exists := snapshot.ChannelByID[target.ChannelID]
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

	return response
}

func validatePublicStatusProbeAdminConfigRequest(request publicStatusProbeAdminConfigUpdateRequest) error {
	fieldErrors := make([]publicStatusProbeAdminFieldError, 0, 6)
	if request.Version <= 0 {
		fieldErrors = appendPublicStatusProbeAdminFieldError(fieldErrors, "version")
	}
	if request.PingTimeoutSeconds < 1 || request.PingTimeoutSeconds > 15 {
		fieldErrors = appendPublicStatusProbeAdminFieldError(fieldErrors, "ping_timeout_seconds")
	}
	if request.ChatTimeoutSeconds < 5 || request.ChatTimeoutSeconds > 60 {
		fieldErrors = appendPublicStatusProbeAdminFieldError(fieldErrors, "chat_timeout_seconds")
	}
	if request.DegradedLatencyMS < 1 || request.DegradedLatencyMS > 60_000 {
		fieldErrors = appendPublicStatusProbeAdminFieldError(fieldErrors, "degraded_latency_ms")
	}
	if request.Concurrency < 1 || request.Concurrency > 20 {
		fieldErrors = appendPublicStatusProbeAdminFieldError(fieldErrors, "concurrency")
	}
	if request.RetentionDays < 1 || request.RetentionDays > 30 {
		fieldErrors = appendPublicStatusProbeAdminFieldError(fieldErrors, "retention_days")
	}
	return newPublicStatusProbeAdminValidationError("invalid public status probe configuration", fieldErrors)
}

func validatePublicStatusProbeTarget(
	c *gin.Context,
	db *gorm.DB,
	target publicstatusprobesetting.Target,
	snapshot publicStatusProbeAdminChannelSnapshot,
	requireAvailableChannel bool,
) error {
	invalidGroup := !validPublicStatusProbeAdminTargetString(target.Group, publicStatusProbeAdminMaxTargetGroupRunes)
	invalidDisplayName := !validPublicStatusProbeAdminTargetString(target.DisplayName, publicStatusProbeAdminMaxTargetDisplayNameRunes)
	invalidModel := !validPublicStatusProbeAdminTargetString(target.Model, publicStatusProbeAdminMaxTargetModelRunes)
	invalidProtocol := !validPublicStatusProbeAdminProtocol(target.Protocol)
	invalidChannelID := target.ChannelID <= 0
	invalidKeyIndex := target.KeyIndex < 0

	channel, channelExists := snapshot.ChannelByID[target.ChannelID]
	if requireAvailableChannel && target.ChannelID > 0 {
		if !channelExists || channel.Status != common.ChannelStatusEnabled {
			invalidChannelID = true
		} else {
			if validPublicStatusProbeAdminProtocol(target.Protocol) && !publicStatusProbeAdminProtocolMatchesChannel(target.Protocol, channel.Type) {
				invalidProtocol = true
			}
			if validPublicStatusProbeAdminTargetString(target.Model, publicStatusProbeAdminMaxTargetModelRunes) && !publicStatusProbeAdminChannelOffersModel(channel.Models, target.Model) {
				invalidModel = true
			}
			if target.KeyIndex >= 0 && ((!channel.IsMultiKey && target.KeyIndex != 0) || (channel.IsMultiKey && target.KeyIndex >= channel.KeyCount)) {
				invalidKeyIndex = true
			}
			if keyStatus, exists := snapshot.ChannelInfoByID[target.ChannelID].MultiKeyStatusList[target.KeyIndex]; exists && keyStatus != common.ChannelStatusEnabled {
				invalidKeyIndex = true
			}
		}
	}

	fieldErrors := make([]publicStatusProbeAdminFieldError, 0, 6)
	for _, invalidField := range []struct {
		invalid bool
		field   string
	}{
		{invalid: invalidGroup, field: "group"},
		{invalid: invalidDisplayName, field: "display_name"},
		{invalid: invalidModel, field: "model"},
		{invalid: invalidProtocol, field: "protocol"},
		{invalid: invalidChannelID, field: "channel_id"},
		{invalid: invalidKeyIndex, field: "key_index"},
	} {
		if invalidField.invalid {
			fieldErrors = appendPublicStatusProbeAdminFieldError(fieldErrors, invalidField.field)
		}
	}
	if validationErr := newPublicStatusProbeAdminValidationError("invalid public status probe target", fieldErrors); validationErr != nil {
		return validationErr
	}
	if !requireAvailableChannel {
		return nil
	}

	loader := probeservice.NewDBTargetLoader(db)
	if err := loader.ValidateTarget(c.Request.Context(), target); err != nil {
		if probeservice.ErrorCodeOf(err) == probeservice.ErrorNetwork {
			return errPublicStatusProbeChannelLoad
		}
		return &publicStatusProbeAdminValidationError{
			Message:     "invalid public status probe target",
			FieldErrors: make([]publicStatusProbeAdminFieldError, 0),
		}
	}
	return nil
}

func validPublicStatusProbeAdminTargetString(value string, maximumRunes int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximumRunes
}

func validPublicStatusProbeAdminProtocol(protocol publicstatusprobesetting.Protocol) bool {
	switch protocol {
	case publicstatusprobesetting.ProtocolOpenAIChat,
		publicstatusprobesetting.ProtocolOpenAIResponses,
		publicstatusprobesetting.ProtocolAnthropicMessages,
		publicstatusprobesetting.ProtocolGeminiGenerateContent:
		return true
	default:
		return false
	}
}

func publicStatusProbeAdminProtocolMatchesChannel(protocol publicstatusprobesetting.Protocol, channelType int) bool {
	switch protocol {
	case publicstatusprobesetting.ProtocolOpenAIChat, publicstatusprobesetting.ProtocolOpenAIResponses:
		return channelType == constant.ChannelTypeOpenAI
	case publicstatusprobesetting.ProtocolAnthropicMessages:
		return channelType == constant.ChannelTypeAnthropic
	case publicstatusprobesetting.ProtocolGeminiGenerateContent:
		return channelType == constant.ChannelTypeGemini
	default:
		return false
	}
}

func publicStatusProbeAdminChannelOffersModel(models string, targetModel string) bool {
	for _, modelName := range strings.Split(models, ",") {
		if strings.TrimSpace(modelName) == targetModel {
			return true
		}
	}
	return false
}

func appendPublicStatusProbeAdminFieldError(fieldErrors []publicStatusProbeAdminFieldError, field string) []publicStatusProbeAdminFieldError {
	for _, fieldError := range fieldErrors {
		if fieldError.Field == field {
			return fieldErrors
		}
	}
	return append(fieldErrors, publicStatusProbeAdminFieldError{
		Field: field,
		Code:  publicStatusProbeAdminFieldErrorInvalid,
	})
}

func newPublicStatusProbeAdminValidationError(message string, fieldErrors []publicStatusProbeAdminFieldError) error {
	if len(fieldErrors) == 0 {
		return nil
	}
	return &publicStatusProbeAdminValidationError{
		Message:     message,
		FieldErrors: fieldErrors,
	}
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
	var validationErr *publicStatusProbeAdminValidationError
	switch {
	case errors.Is(err, errPublicStatusProbeTargetMissing), errors.Is(err, gorm.ErrRecordNotFound):
		writePublicStatusProbeAdminError(c, http.StatusNotFound, "public status probe target not found")
	case errors.Is(err, model.ErrPublicStatusProbeConfigConflict):
		writePublicStatusProbeAdminConflict(c)
	case errors.As(err, &validationErr):
		writePublicStatusProbeAdminValidationError(c, validationErr)
	case errors.Is(err, errPublicStatusProbeChannelLoad):
		writePublicStatusProbeAdminError(c, http.StatusInternalServerError, "failed to load public status probe config")
	default:
		switch probeservice.ErrorCodeOf(err) {
		case probeservice.ErrorInvalidTarget, probeservice.ErrorUnsupportedProvider, probeservice.ErrorValidationFailed, probeservice.ErrorResponseTooLarge:
			writePublicStatusProbeAdminValidationError(c, &publicStatusProbeAdminValidationError{
				Message:     "invalid public status probe target",
				FieldErrors: make([]publicStatusProbeAdminFieldError, 0),
			})
		case probeservice.ErrorNetwork, probeservice.ErrorTimeout, probeservice.ErrorEmptyResponse, probeservice.ErrorProviderRejected:
			writePublicStatusProbeAdminError(c, http.StatusInternalServerError, "failed to update public status probe config")
		default:
			writePublicStatusProbeAdminError(c, http.StatusInternalServerError, "failed to update public status probe config")
		}
	}
}

func writePublicStatusProbeAdminValidationError(c *gin.Context, err *publicStatusProbeAdminValidationError) {
	response := publicStatusProbeAdminValidationResponse{
		Success: false,
		Message: err.Message,
	}
	response.Data.FieldErrors = err.FieldErrors
	c.AbortWithStatusJSON(http.StatusBadRequest, response)
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
