package group_probe_setting

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSetting(t *testing.T) {
	got := DefaultSetting()
	assert.False(t, got.Enabled)
	assert.Equal(t, 10, got.IntervalMinutes)
	assert.Equal(t, 7, got.RetentionDays)
	assert.Equal(t, 45, got.TimeoutSeconds)
	assert.Empty(t, got.Groups)

	registered := config.GlobalConfig.Get(ConfigName)
	require.NotNil(t, registered)
	values, err := config.ConfigToMap(registered)
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.Contains(t, values, ConfigField)
	assert.Equal(t, ConfigName+"."+ConfigField, OptionKey)
}

func TestValidateAndNormalizeAcceptsBoundariesAndTrims(t *testing.T) {
	candidate := Setting{
		Enabled:         true,
		IntervalMinutes: 5,
		RetentionDays:   90,
		TimeoutSeconds:  120,
		Groups: []GroupMapping{{
			Group:       "  codex  ",
			DisplayName: "  Codex PRO  ",
			Model:       "  gpt-5.5  ",
			Public:      true,
		}},
	}
	var validatedGroup, validatedModel string
	got, err := ValidateAndNormalize(candidate, func(group, model string) (bool, error) {
		validatedGroup, validatedModel = group, model
		return true, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "codex", got.Groups[0].Group)
	assert.Equal(t, "Codex PRO", got.Groups[0].DisplayName)
	assert.Equal(t, "gpt-5.5", got.Groups[0].Model)
	assert.True(t, got.Groups[0].Public)
	assert.Equal(t, "codex", validatedGroup)
	assert.Equal(t, "gpt-5.5", validatedModel)

	candidate.IntervalMinutes = 1440
	candidate.RetentionDays = 1
	candidate.TimeoutSeconds = 5
	_, err = ValidateAndNormalize(candidate, func(string, string) (bool, error) { return true, nil })
	require.NoError(t, err)
}

func TestValidateAndNormalizeRejectsNumericValuesOutsideBounds(t *testing.T) {
	base := DefaultSetting()
	tests := []struct {
		name   string
		mutate func(*Setting)
	}{
		{"interval below", func(s *Setting) { s.IntervalMinutes = 4 }},
		{"interval above", func(s *Setting) { s.IntervalMinutes = 1441 }},
		{"retention below", func(s *Setting) { s.RetentionDays = 0 }},
		{"retention above", func(s *Setting) { s.RetentionDays = 91 }},
		{"timeout below", func(s *Setting) { s.TimeoutSeconds = 4 }},
		{"timeout above", func(s *Setting) { s.TimeoutSeconds = 121 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			tc.mutate(&candidate)
			_, err := ValidateAndNormalize(candidate, nil)
			assert.Error(t, err)
		})
	}
}

func TestValidateAndNormalizeRejectsInvalidMappings(t *testing.T) {
	valid := GroupMapping{Group: "codex", DisplayName: "Codex", Model: "gpt-5.5"}
	tests := []struct {
		name    string
		mapping GroupMapping
	}{
		{"empty group", GroupMapping{Group: "  ", DisplayName: "Codex", Model: "gpt-5.5"}},
		{"empty display", GroupMapping{Group: "codex", DisplayName: "  ", Model: "gpt-5.5"}},
		{"empty model", GroupMapping{Group: "codex", DisplayName: "Codex", Model: "  "}},
		{"long group", GroupMapping{Group: strings.Repeat("g", MaxGroupNameLength+1), DisplayName: "Codex", Model: "gpt-5.5"}},
		{"long display", GroupMapping{Group: "codex", DisplayName: strings.Repeat("d", MaxDisplayNameLength+1), Model: "gpt-5.5"}},
		{"long model", GroupMapping{Group: "codex", DisplayName: "Codex", Model: strings.Repeat("m", MaxModelNameLength+1)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := DefaultSetting()
			candidate.Groups = []GroupMapping{tc.mapping}
			_, err := ValidateAndNormalize(candidate, nil)
			assert.Error(t, err)
		})
	}

	candidate := DefaultSetting()
	candidate.Groups = []GroupMapping{valid, GroupMapping{Group: " codex ", DisplayName: "Other", Model: "gpt-5.5"}}
	_, err := ValidateAndNormalize(candidate, nil)
	assert.ErrorContains(t, err, "duplicate")
}

func TestValidateAndNormalizeEnforcesMaximumMappings(t *testing.T) {
	candidate := DefaultSetting()
	for i := 0; i < MaxGroups; i++ {
		candidate.Groups = append(candidate.Groups, GroupMapping{
			Group:       "group-" + strings.Repeat("x", i),
			DisplayName: "Group",
			Model:       "model",
		})
	}
	_, err := ValidateAndNormalize(candidate, nil)
	require.NoError(t, err)

	candidate.Groups = append(candidate.Groups, GroupMapping{Group: "overflow", DisplayName: "Overflow", Model: "model"})
	_, err = ValidateAndNormalize(candidate, nil)
	assert.Error(t, err)
}

func TestValidateAndNormalizeChecksEnabledAbility(t *testing.T) {
	candidate := DefaultSetting()
	candidate.Groups = []GroupMapping{{Group: "codex", DisplayName: "Codex", Model: "gpt-5.5"}}

	_, err := ValidateAndNormalize(candidate, func(string, string) (bool, error) { return false, nil })
	assert.ErrorContains(t, err, "enabled ability")

	sentinel := errors.New("database unavailable")
	_, err = ValidateAndNormalize(candidate, func(string, string) (bool, error) { return false, sentinel })
	assert.ErrorIs(t, err, sentinel)
}

func TestSettingJSONRoundTripUsesNormalizedValues(t *testing.T) {
	candidate := DefaultSetting()
	candidate.Enabled = true
	candidate.Groups = []GroupMapping{{Group: " codex ", DisplayName: " Codex ", Model: " gpt-5.5 ", Public: true}}

	raw, err := Encode(candidate)
	require.NoError(t, err)
	got, err := Decode(raw, func(string, string) (bool, error) { return true, nil })
	require.NoError(t, err)
	assert.Equal(t, "codex", got.Groups[0].Group)
	assert.Equal(t, "Codex", got.Groups[0].DisplayName)
	assert.Equal(t, "gpt-5.5", got.Groups[0].Model)
	_, err = Decode(`{"interval_minutes":4}`, nil)
	assert.Error(t, err)
}
