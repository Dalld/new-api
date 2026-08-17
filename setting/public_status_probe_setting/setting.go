package public_status_probe_setting

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	envEnabled            = "PUBLIC_STATUS_PROBE_ENABLED"
	envIntervalSeconds    = "PUBLIC_STATUS_PROBE_INTERVAL_SECONDS"
	envPingTimeoutSeconds = "PUBLIC_STATUS_PROBE_PING_TIMEOUT_SECONDS"
	envChatTimeoutSeconds = "PUBLIC_STATUS_PROBE_CHAT_TIMEOUT_SECONDS"
	envDegradedMS         = "PUBLIC_STATUS_PROBE_DEGRADED_MS"
	envConcurrency        = "PUBLIC_STATUS_PROBE_CONCURRENCY"
	envRetentionDays      = "PUBLIC_STATUS_PROBE_RETENTION_DAYS"
	envTargets            = "PUBLIC_STATUS_PROBE_TARGETS"

	maxTargetsJSONBytes = 64 * 1024
	maxTargets          = 20
	maxKeyRunes         = 96
	maxGroupRunes       = 64
	maxDisplayNameRunes = 128
	maxModelRunes       = 128
	maxDegradedMS       = 60_000

	invalidConfigurationErrorText = "invalid public status probe configuration"
)

type Protocol string

const (
	ProtocolOpenAIChat            Protocol = "openai_chat"
	ProtocolOpenAIResponses       Protocol = "openai_responses"
	ProtocolAnthropicMessages     Protocol = "anthropic_messages"
	ProtocolGeminiGenerateContent Protocol = "gemini_generate_content"
)

type Target struct {
	Enabled     bool     `json:"enabled"`
	Key         string   `json:"key"`
	Group       string   `json:"group"`
	DisplayName string   `json:"display_name"`
	Model       string   `json:"model"`
	Protocol    Protocol `json:"protocol"`
	ChannelID   int      `json:"channel_id"`
	KeyIndex    int      `json:"key_index"`
}

type Setting struct {
	Enabled         bool
	Interval        time.Duration
	PingTimeout     time.Duration
	ChatTimeout     time.Duration
	DegradedLatency time.Duration
	Concurrency     int
	RetentionDays   int
	Targets         []Target
}

type rawTarget struct {
	Enabled     *bool    `json:"enabled"`
	Key         string   `json:"key"`
	Group       string   `json:"group"`
	DisplayName string   `json:"display_name"`
	Model       string   `json:"model"`
	Protocol    Protocol `json:"protocol"`
	ChannelID   int      `json:"channel_id"`
	KeyIndex    *int     `json:"key_index"`
}

func Load() (Setting, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Setting, error) {
	enabled, err := boolSetting(lookup, envEnabled, false)
	if err != nil {
		return Setting{}, errors.New(invalidConfigurationErrorText)
	}
	intervalSeconds, err := intSetting(lookup, envIntervalSeconds, 60, 60, 60)
	if err != nil {
		return Setting{}, errors.New(invalidConfigurationErrorText)
	}
	pingTimeoutSeconds, err := intSetting(lookup, envPingTimeoutSeconds, 8, 1, 15)
	if err != nil {
		return Setting{}, errors.New(invalidConfigurationErrorText)
	}
	chatTimeoutSeconds, err := intSetting(lookup, envChatTimeoutSeconds, 45, 5, 60)
	if err != nil {
		return Setting{}, errors.New(invalidConfigurationErrorText)
	}
	degradedMS, err := intSetting(lookup, envDegradedMS, 6000, 1, maxDegradedMS)
	if err != nil {
		return Setting{}, errors.New(invalidConfigurationErrorText)
	}
	concurrency, err := intSetting(lookup, envConcurrency, 5, 1, 20)
	if err != nil {
		return Setting{}, errors.New(invalidConfigurationErrorText)
	}
	retentionDays, err := intSetting(lookup, envRetentionDays, 7, 1, 30)
	if err != nil {
		return Setting{}, errors.New(invalidConfigurationErrorText)
	}
	targets, err := targetSetting(lookup)
	if err != nil {
		return Setting{}, errors.New(invalidConfigurationErrorText)
	}

	return Setting{
		Enabled:         enabled,
		Interval:        time.Duration(intervalSeconds) * time.Second,
		PingTimeout:     time.Duration(pingTimeoutSeconds) * time.Second,
		ChatTimeout:     time.Duration(chatTimeoutSeconds) * time.Second,
		DegradedLatency: time.Duration(degradedMS) * time.Millisecond,
		Concurrency:     concurrency,
		RetentionDays:   retentionDays,
		Targets:         targets,
	}, nil
}

func boolSetting(lookup func(string) (string, bool), name string, defaultValue bool) (bool, error) {
	value, ok := lookup(name)
	if !ok {
		return defaultValue, nil
	}
	return strconv.ParseBool(strings.TrimSpace(value))
}

func intSetting(lookup func(string) (string, bool), name string, defaultValue, minimum, maximum int) (int, error) {
	value, ok := lookup(name)
	if !ok {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, errors.New(invalidConfigurationErrorText)
	}
	return parsed, nil
}

func targetSetting(lookup func(string) (string, bool)) ([]Target, error) {
	raw, ok := lookup(envTargets)
	if !ok {
		raw = "[]"
	}
	if len(raw) > maxTargetsJSONBytes || !utf8.ValidString(raw) || common.GetJsonType(json.RawMessage(raw)) != "array" {
		return nil, errors.New(invalidConfigurationErrorText)
	}

	var rawTargets []map[string]json.RawMessage
	if err := common.Unmarshal([]byte(raw), &rawTargets); err != nil || len(rawTargets) > maxTargets {
		return nil, errors.New(invalidConfigurationErrorText)
	}

	decoded := make([]rawTarget, 0, len(rawTargets))
	for _, fields := range rawTargets {
		for field := range fields {
			switch field {
			case "enabled", "key", "group", "display_name", "model", "protocol", "channel_id", "key_index":
			default:
				return nil, errors.New(invalidConfigurationErrorText)
			}
		}
		if !validTargetJSONFieldTypes(fields) {
			return nil, errors.New(invalidConfigurationErrorText)
		}

		encoded, err := common.Marshal(fields)
		if err != nil {
			return nil, errors.New(invalidConfigurationErrorText)
		}
		var target rawTarget
		if err := common.Unmarshal(encoded, &target); err != nil {
			return nil, errors.New(invalidConfigurationErrorText)
		}
		decoded = append(decoded, target)
	}

	targets := make([]Target, 0, len(decoded))
	keys := make(map[string]struct{}, len(decoded))
	for _, rawTarget := range decoded {
		key := strings.TrimSpace(rawTarget.Key)
		group := strings.TrimSpace(rawTarget.Group)
		displayName := strings.TrimSpace(rawTarget.DisplayName)
		model := strings.TrimSpace(rawTarget.Model)
		protocol := Protocol(strings.TrimSpace(string(rawTarget.Protocol)))
		if !validTargetString(key, maxKeyRunes) ||
			!validTargetString(group, maxGroupRunes) ||
			!validTargetString(displayName, maxDisplayNameRunes) ||
			!validTargetString(model, maxModelRunes) ||
			!validProtocol(protocol) ||
			rawTarget.ChannelID <= 0 {
			return nil, errors.New(invalidConfigurationErrorText)
		}
		if _, exists := keys[key]; exists {
			return nil, errors.New(invalidConfigurationErrorText)
		}

		keyIndex := 0
		if rawTarget.KeyIndex != nil {
			keyIndex = *rawTarget.KeyIndex
		}
		if keyIndex < 0 {
			return nil, errors.New(invalidConfigurationErrorText)
		}
		enabled := true
		if rawTarget.Enabled != nil {
			enabled = *rawTarget.Enabled
		}

		keys[key] = struct{}{}
		targets = append(targets, Target{
			Enabled:     enabled,
			Key:         key,
			Group:       group,
			DisplayName: displayName,
			Model:       model,
			Protocol:    protocol,
			ChannelID:   rawTarget.ChannelID,
			KeyIndex:    keyIndex,
		})
	}
	return targets, nil
}

func validTargetString(value string, maximumRunes int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximumRunes
}

func validTargetJSONFieldTypes(fields map[string]json.RawMessage) bool {
	if common.GetJsonType(fields["key"]) != "string" ||
		common.GetJsonType(fields["group"]) != "string" ||
		common.GetJsonType(fields["display_name"]) != "string" ||
		common.GetJsonType(fields["model"]) != "string" ||
		common.GetJsonType(fields["protocol"]) != "string" ||
		common.GetJsonType(fields["channel_id"]) != "number" {
		return false
	}
	if value, exists := fields["enabled"]; exists && common.GetJsonType(value) != "boolean" {
		return false
	}
	if value, exists := fields["key_index"]; exists && common.GetJsonType(value) != "number" {
		return false
	}
	return true
}

func validProtocol(value Protocol) bool {
	if !utf8.ValidString(string(value)) {
		return false
	}
	switch value {
	case ProtocolOpenAIChat, ProtocolOpenAIResponses, ProtocolAnthropicMessages, ProtocolGeminiGenerateContent:
		return true
	default:
		return false
	}
}
