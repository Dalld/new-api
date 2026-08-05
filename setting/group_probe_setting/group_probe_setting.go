package group_probe_setting

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ConfigName  = "group_probe_setting"
	ConfigField = "config"
	OptionKey   = ConfigName + "." + ConfigField

	MinIntervalMinutes = 5
	MaxIntervalMinutes = 1440
	MinRetentionDays   = 1
	MaxRetentionDays   = 90
	MinTimeoutSeconds  = 5
	MaxTimeoutSeconds  = 120
	MaxGroups          = 50

	MaxGroupNameLength   = 64
	MaxDisplayNameLength = 128
	MaxModelNameLength   = 255
)

type GroupMapping struct {
	Group       string `json:"group"`
	DisplayName string `json:"display_name"`
	Model       string `json:"model"`
	Public      bool   `json:"public"`
}

type Setting struct {
	Enabled         bool           `json:"enabled"`
	IntervalMinutes int            `json:"interval_minutes"`
	RetentionDays   int            `json:"retention_days"`
	TimeoutSeconds  int            `json:"timeout_seconds"`
	Groups          []GroupMapping `json:"groups"`
}

type AbilityValidator func(group, model string) (bool, error)

type registeredSetting struct {
	Config Setting `json:"config"`
}

var (
	groupProbeSetting   = registeredSetting{Config: DefaultSetting()}
	groupProbeSettingMu sync.RWMutex
)

func init() {
	config.GlobalConfig.Register(ConfigName, &groupProbeSetting)
}

func DefaultSetting() Setting {
	return Setting{
		Enabled:         false,
		IntervalMinutes: 10,
		RetentionDays:   7,
		TimeoutSeconds:  45,
		Groups:          []GroupMapping{},
	}
}

func GetSetting() Setting {
	groupProbeSettingMu.RLock()
	current := cloneSetting(groupProbeSetting.Config)
	groupProbeSettingMu.RUnlock()
	return current
}

func SetSetting(candidate Setting, validator AbilityValidator) error {
	normalized, err := ValidateAndNormalize(candidate, validator)
	if err != nil {
		return err
	}
	groupProbeSettingMu.Lock()
	groupProbeSetting.Config = cloneSetting(normalized)
	groupProbeSettingMu.Unlock()
	return nil
}

func ValidateAndNormalize(candidate Setting, validator AbilityValidator) (Setting, error) {
	if candidate.IntervalMinutes < MinIntervalMinutes || candidate.IntervalMinutes > MaxIntervalMinutes {
		return Setting{}, fmt.Errorf("interval_minutes must be between %d and %d", MinIntervalMinutes, MaxIntervalMinutes)
	}
	if candidate.RetentionDays < MinRetentionDays || candidate.RetentionDays > MaxRetentionDays {
		return Setting{}, fmt.Errorf("retention_days must be between %d and %d", MinRetentionDays, MaxRetentionDays)
	}
	if candidate.TimeoutSeconds < MinTimeoutSeconds || candidate.TimeoutSeconds > MaxTimeoutSeconds {
		return Setting{}, fmt.Errorf("timeout_seconds must be between %d and %d", MinTimeoutSeconds, MaxTimeoutSeconds)
	}
	if len(candidate.Groups) > MaxGroups {
		return Setting{}, fmt.Errorf("groups must contain at most %d mappings", MaxGroups)
	}

	normalized := candidate
	normalized.Groups = make([]GroupMapping, len(candidate.Groups))
	seen := make(map[string]struct{}, len(candidate.Groups))
	for i, mapping := range candidate.Groups {
		mapping.Group = strings.TrimSpace(mapping.Group)
		mapping.DisplayName = strings.TrimSpace(mapping.DisplayName)
		mapping.Model = strings.TrimSpace(mapping.Model)

		if err := validateTextField("group", mapping.Group, MaxGroupNameLength); err != nil {
			return Setting{}, fmt.Errorf("groups[%d]: %w", i, err)
		}
		if err := validateTextField("display_name", mapping.DisplayName, MaxDisplayNameLength); err != nil {
			return Setting{}, fmt.Errorf("groups[%d]: %w", i, err)
		}
		if err := validateTextField("model", mapping.Model, MaxModelNameLength); err != nil {
			return Setting{}, fmt.Errorf("groups[%d]: %w", i, err)
		}
		if _, exists := seen[mapping.Group]; exists {
			return Setting{}, fmt.Errorf("groups[%d]: duplicate group %q", i, mapping.Group)
		}
		seen[mapping.Group] = struct{}{}

		if validator != nil {
			enabled, err := validator(mapping.Group, mapping.Model)
			if err != nil {
				return Setting{}, fmt.Errorf("groups[%d]: validate enabled ability: %w", i, err)
			}
			if !enabled {
				return Setting{}, fmt.Errorf("groups[%d]: no enabled ability for group %q and model %q", i, mapping.Group, mapping.Model)
			}
		}
		normalized.Groups[i] = mapping
	}
	return normalized, nil
}

func Encode(candidate Setting) (string, error) {
	normalized, err := ValidateAndNormalize(candidate, nil)
	if err != nil {
		return "", err
	}
	raw, err := common.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func Decode(raw string, validator AbilityValidator) (Setting, error) {
	decoded := DefaultSetting()
	if err := common.Unmarshal([]byte(raw), &decoded); err != nil {
		return Setting{}, fmt.Errorf("decode group probe setting: %w", err)
	}
	return ValidateAndNormalize(decoded, validator)
}

func (setting Setting) MarshalJSON() ([]byte, error) {
	type settingAlias Setting
	return common.Marshal(settingAlias(setting))
}

func (setting *Setting) UnmarshalJSON(data []byte) error {
	type settingAlias Setting
	decoded := settingAlias(DefaultSetting())
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	normalized, err := ValidateAndNormalize(Setting(decoded), nil)
	if err != nil {
		return err
	}
	*setting = normalized
	return nil
}

func validateTextField(name, value string, maxLength int) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", name)
	}
	if utf8.RuneCountInString(value) > maxLength {
		return fmt.Errorf("%s must contain at most %d characters", name, maxLength)
	}
	return nil
}

func cloneSetting(source Setting) Setting {
	cloned := source
	cloned.Groups = append([]GroupMapping(nil), source.Groups...)
	if cloned.Groups == nil {
		cloned.Groups = []GroupMapping{}
	}
	return cloned
}
