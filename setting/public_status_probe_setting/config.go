package public_status_probe_setting

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	OptionKey     = "PublicStatusProbeConfig"
	SchemaVersion = 1

	maxDocumentJSONBytes = 64 * 1024
)

type Document struct {
	SchemaVersion      int      `json:"schema_version"`
	Version            int64    `json:"version"`
	Enabled            bool     `json:"enabled"`
	PingTimeoutSeconds int      `json:"ping_timeout_seconds"`
	ChatTimeoutSeconds int      `json:"chat_timeout_seconds"`
	DegradedLatencyMS  int      `json:"degraded_latency_ms"`
	Concurrency        int      `json:"concurrency"`
	RetentionDays      int      `json:"retention_days"`
	Targets            []Target `json:"targets"`
}

type publishNotification struct {
	version int64
	hook    func(int64)
}

var (
	runtimeMu          sync.RWMutex
	publishMu          sync.Mutex
	runtimeDocument    = DefaultDocument()
	publishHook        func(int64)
	publishQueue       []publishNotification
	publishDispatching bool
)

func DefaultDocument() Document {
	return Document{
		SchemaVersion:      SchemaVersion,
		Version:            1,
		Enabled:            false,
		PingTimeoutSeconds: 8,
		ChatTimeoutSeconds: 45,
		DegradedLatencyMS:  6000,
		Concurrency:        5,
		RetentionDays:      7,
		Targets:            make([]Target, 0),
	}
}

func DecodeDocument(raw string) (Document, error) {
	if len(raw) > maxDocumentJSONBytes || !utf8.ValidString(raw) || common.GetJsonType(json.RawMessage(raw)) != "object" {
		return Document{}, invalidConfigurationError()
	}
	duplicate, err := hasDuplicateJSONObjectMembers(raw)
	if err != nil || duplicate {
		return Document{}, invalidConfigurationError()
	}

	var fields map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(raw, &fields); err != nil || len(fields) != 9 {
		return Document{}, invalidConfigurationError()
	}
	for field := range fields {
		switch field {
		case "schema_version", "version", "enabled", "ping_timeout_seconds", "chat_timeout_seconds", "degraded_latency_ms", "concurrency", "retention_days", "targets":
		default:
			return Document{}, invalidConfigurationError()
		}
	}
	if common.GetJsonType(fields["schema_version"]) != "number" ||
		common.GetJsonType(fields["version"]) != "number" ||
		common.GetJsonType(fields["enabled"]) != "boolean" ||
		common.GetJsonType(fields["ping_timeout_seconds"]) != "number" ||
		common.GetJsonType(fields["chat_timeout_seconds"]) != "number" ||
		common.GetJsonType(fields["degraded_latency_ms"]) != "number" ||
		common.GetJsonType(fields["concurrency"]) != "number" ||
		common.GetJsonType(fields["retention_days"]) != "number" ||
		common.GetJsonType(fields["targets"]) != "array" {
		return Document{}, invalidConfigurationError()
	}

	targetData := fields["targets"]
	var targetFields []map[string]json.RawMessage
	if err := common.Unmarshal(targetData, &targetFields); err != nil || len(targetFields) > maxTargets {
		return Document{}, invalidConfigurationError()
	}

	targets := make([]Target, 0, len(targetFields))
	for _, fields := range targetFields {
		if len(fields) != 8 {
			return Document{}, invalidConfigurationError()
		}
		for field := range fields {
			switch field {
			case "enabled", "key", "group", "display_name", "model", "protocol", "channel_id", "key_index":
			default:
				return Document{}, invalidConfigurationError()
			}
		}
		if !validTargetJSONFieldTypes(fields) {
			return Document{}, invalidConfigurationError()
		}

		encoded, err := common.Marshal(fields)
		if err != nil {
			return Document{}, invalidConfigurationError()
		}
		var target Target
		if err := common.Unmarshal(encoded, &target); err != nil {
			return Document{}, invalidConfigurationError()
		}
		targets = append(targets, target)
	}

	encoded, err := common.Marshal(fields)
	if err != nil {
		return Document{}, invalidConfigurationError()
	}
	var document Document
	if err := common.Unmarshal(encoded, &document); err != nil {
		return Document{}, invalidConfigurationError()
	}
	document.Targets = targets
	return ValidateAndNormalizeDocument(document)
}

func EncodeDocument(document Document) (string, error) {
	normalized, err := ValidateAndNormalizeDocument(document)
	if err != nil {
		return "", err
	}
	encoded, err := common.Marshal(normalized)
	if err != nil {
		return "", invalidConfigurationError()
	}
	return string(encoded), nil
}

func ValidateAndNormalizeDocument(document Document) (Document, error) {
	if document.SchemaVersion != SchemaVersion ||
		document.Version <= 0 ||
		document.PingTimeoutSeconds < 1 || document.PingTimeoutSeconds > 15 ||
		document.ChatTimeoutSeconds < 5 || document.ChatTimeoutSeconds > 60 ||
		document.DegradedLatencyMS < 1 || document.DegradedLatencyMS > maxDegradedMS ||
		document.Concurrency < 1 || document.Concurrency > 20 ||
		document.RetentionDays < 1 || document.RetentionDays > 30 ||
		len(document.Targets) > maxTargets {
		return Document{}, invalidConfigurationError()
	}

	normalized := document
	normalized.Targets = make([]Target, 0, len(document.Targets))
	keys := make(map[string]struct{}, len(document.Targets))
	for _, target := range document.Targets {
		target.Key = strings.TrimSpace(target.Key)
		target.Group = strings.TrimSpace(target.Group)
		target.DisplayName = strings.TrimSpace(target.DisplayName)
		target.Model = strings.TrimSpace(target.Model)
		target.Protocol = Protocol(strings.TrimSpace(string(target.Protocol)))
		if !validTargetString(target.Key, maxKeyRunes) ||
			!validTargetString(target.Group, maxGroupRunes) ||
			!validTargetString(target.DisplayName, maxDisplayNameRunes) ||
			!validTargetString(target.Model, maxModelRunes) ||
			!validProtocol(target.Protocol) ||
			target.ChannelID <= 0 ||
			target.KeyIndex < 0 {
			return Document{}, invalidConfigurationError()
		}
		if _, exists := keys[target.Key]; exists {
			return Document{}, invalidConfigurationError()
		}
		keys[target.Key] = struct{}{}
		normalized.Targets = append(normalized.Targets, target)
	}
	return normalized, nil
}

func PublishDocument(document Document) error {
	normalized, err := ValidateAndNormalizeDocument(document)
	if err != nil {
		return err
	}

	publishMu.Lock()
	runtimeMu.Lock()
	runtimeDocument = cloneDocument(normalized)
	hook := publishHook
	runtimeMu.Unlock()
	publishQueue = append(publishQueue, publishNotification{version: normalized.Version, hook: hook})
	if publishDispatching {
		publishMu.Unlock()
		return nil
	}
	publishDispatching = true
	publishMu.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			publishMu.Lock()
			publishDispatching = false
			publishMu.Unlock()
			panic(recovered)
		}
	}()

	for {
		publishMu.Lock()
		if len(publishQueue) == 0 {
			publishQueue = nil
			publishDispatching = false
			publishMu.Unlock()
			return nil
		}
		notification := publishQueue[0]
		publishQueue[0] = publishNotification{}
		publishQueue = publishQueue[1:]
		publishMu.Unlock()

		if notification.hook != nil {
			notification.hook(notification.version)
		}
	}
}

func CurrentDocument() Document {
	runtimeMu.RLock()
	document := cloneDocument(runtimeDocument)
	runtimeMu.RUnlock()
	return document
}

func CurrentSetting() Setting {
	document := CurrentDocument()
	targets := make([]Target, 0, len(document.Targets))
	for _, target := range document.Targets {
		if target.Enabled {
			targets = append(targets, target)
		}
	}
	return Setting{
		Enabled:         document.Enabled,
		Interval:        time.Minute,
		PingTimeout:     time.Duration(document.PingTimeoutSeconds) * time.Second,
		ChatTimeout:     time.Duration(document.ChatTimeoutSeconds) * time.Second,
		DegradedLatency: time.Duration(document.DegradedLatencyMS) * time.Millisecond,
		Concurrency:     document.Concurrency,
		RetentionDays:   document.RetentionDays,
		Targets:         targets,
	}
}

func LoadEnvironmentDocument() (Document, error) {
	return loadEnvironmentDocument(os.LookupEnv)
}

func loadEnvironmentDocument(lookup func(string) (string, bool)) (Document, error) {
	setting, err := load(lookup)
	if err != nil {
		return Document{}, err
	}
	document := DefaultDocument()
	document.Enabled = setting.Enabled
	document.PingTimeoutSeconds = int(setting.PingTimeout / time.Second)
	document.ChatTimeoutSeconds = int(setting.ChatTimeout / time.Second)
	document.DegradedLatencyMS = int(setting.DegradedLatency / time.Millisecond)
	document.Concurrency = setting.Concurrency
	document.RetentionDays = setting.RetentionDays
	document.Targets = cloneTargets(setting.Targets)
	return ValidateAndNormalizeDocument(document)
}

func SetPublishHook(hook func(int64)) {
	runtimeMu.Lock()
	publishHook = hook
	runtimeMu.Unlock()
}

func cloneDocument(document Document) Document {
	document.Targets = cloneTargets(document.Targets)
	return document
}

func cloneTargets(targets []Target) []Target {
	cloned := make([]Target, len(targets))
	copy(cloned, targets)
	return cloned
}

func invalidConfigurationError() error {
	return errors.New(invalidConfigurationErrorText)
}

func hasDuplicateJSONObjectMembers(raw string) (bool, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	return scanJSONValueForDuplicateMembers(decoder)
}

func scanJSONValueForDuplicateMembers(decoder *json.Decoder) (bool, error) {
	token, err := decoder.Token()
	if err != nil {
		return false, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return false, nil
	}

	switch delimiter {
	case '{':
		members := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return false, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return false, errors.New("invalid JSON object member")
			}
			if _, exists := members[key]; exists {
				return true, nil
			}
			members[key] = struct{}{}
			duplicate, err := scanJSONValueForDuplicateMembers(decoder)
			if err != nil || duplicate {
				return duplicate, err
			}
		}
		_, err = decoder.Token()
		return false, err
	case '[':
		for decoder.More() {
			duplicate, err := scanJSONValueForDuplicateMembers(decoder)
			if err != nil || duplicate {
				return duplicate, err
			}
		}
		_, err = decoder.Token()
		return false, err
	default:
		return false, errors.New("invalid JSON delimiter")
	}
}
