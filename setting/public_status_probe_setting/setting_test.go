package public_status_probe_setting

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validTargetJSON = `[{"key":"target-a","group":"default","display_name":"Target A","model":"gpt-test","protocol":"openai_chat","channel_id":1}]`

func loadWithEnvironment(values map[string]string) (Setting, error) {
	return load(func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	})
}

func TestLoadDefaults(t *testing.T) {
	setting, err := loadWithEnvironment(map[string]string{})
	require.NoError(t, err)

	assert.False(t, setting.Enabled)
	assert.Equal(t, 60*time.Second, setting.Interval)
	assert.Equal(t, 8*time.Second, setting.PingTimeout)
	assert.Equal(t, 45*time.Second, setting.ChatTimeout)
	assert.Equal(t, 6*time.Second, setting.DegradedLatency)
	assert.Equal(t, 5, setting.Concurrency)
	assert.Equal(t, 7, setting.RetentionDays)
	assert.Empty(t, setting.Targets)
	assert.NotNil(t, setting.Targets)
}

func TestLoadAppliesExplicitScalarSettings(t *testing.T) {
	setting, err := loadWithEnvironment(map[string]string{
		envEnabled:            "true",
		envIntervalSeconds:    "60",
		envPingTimeoutSeconds: "15",
		envChatTimeoutSeconds: "60",
		envDegradedMS:         "60000",
		envConcurrency:        "20",
		envRetentionDays:      "30",
		envTargets:            validTargetJSON,
	})
	require.NoError(t, err)
	assert.True(t, setting.Enabled)
	assert.Equal(t, 60*time.Second, setting.Interval)
	assert.Equal(t, 15*time.Second, setting.PingTimeout)
	assert.Equal(t, 60*time.Second, setting.ChatTimeout)
	assert.Equal(t, 60*time.Second, setting.DegradedLatency)
	assert.Equal(t, 20, setting.Concurrency)
	assert.Equal(t, 30, setting.RetentionDays)
	assert.Len(t, setting.Targets, 1)
}

func TestLoadNumericBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		valid       []int
		invalid     []int
	}{
		{name: "fixed interval", environment: envIntervalSeconds, valid: []int{60}, invalid: []int{59, 61}},
		{name: "ping timeout", environment: envPingTimeoutSeconds, valid: []int{1, 15}, invalid: []int{0, 16}},
		{name: "chat timeout", environment: envChatTimeoutSeconds, valid: []int{5, 60}, invalid: []int{4, 61}},
		{name: "degraded latency", environment: envDegradedMS, valid: []int{1, 60000}, invalid: []int{-1, 0, 60001}},
		{name: "concurrency", environment: envConcurrency, valid: []int{1, 20}, invalid: []int{0, 21}},
		{name: "retention", environment: envRetentionDays, valid: []int{1, 30}, invalid: []int{0, 31}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, value := range tt.valid {
				t.Run("accepts_"+strconv.Itoa(value), func(t *testing.T) {
					_, err := loadWithEnvironment(map[string]string{tt.environment: strconv.Itoa(value)})
					require.NoError(t, err)
				})
			}
			for _, value := range tt.invalid {
				t.Run("rejects_"+strconv.Itoa(value), func(t *testing.T) {
					_, err := loadWithEnvironment(map[string]string{tt.environment: strconv.Itoa(value)})
					require.Error(t, err)
				})
			}
		})
	}
}

func TestLoadRejectsMalformedScalarSettings(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		value       string
	}{
		{name: "enabled", environment: envEnabled, value: "sometimes"},
		{name: "interval", environment: envIntervalSeconds, value: "one minute"},
		{name: "ping timeout", environment: envPingTimeoutSeconds, value: "8s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnvironment(map[string]string{tt.environment: tt.value})
			require.Error(t, err)
		})
	}
}

func TestLoadAcceptsTargetCountLimit(t *testing.T) {
	for _, count := range []int{20, 21} {
		t.Run(strconv.Itoa(count)+"_targets", func(t *testing.T) {
			targets := make([]Target, 0, count)
			for i := 0; i < count; i++ {
				targets = append(targets, Target{
					Key:         "target-" + strconv.Itoa(i),
					Group:       "default",
					DisplayName: "Target " + strconv.Itoa(i),
					Model:       "gpt-test",
					Protocol:    "openai_chat",
					ChannelID:   i + 1,
				})
			}
			encoded, err := common.Marshal(targets)
			require.NoError(t, err)

			setting, err := loadWithEnvironment(map[string]string{envTargets: string(encoded)})
			if count == 20 {
				require.NoError(t, err)
				assert.Len(t, setting.Targets, 20)
				return
			}
			require.Error(t, err)
		})
	}
}

func TestLoadValidatesRequiredTargetFields(t *testing.T) {
	tests := []struct {
		name  string
		field string
	}{
		{name: "key", field: "key"},
		{name: "group", field: "group"},
		{name: "display name", field: "display_name"},
		{name: "model", field: "model"},
		{name: "protocol", field: "protocol"},
		{name: "channel id", field: "channel_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := map[string]any{
				"key":          "target-a",
				"group":        "default",
				"display_name": "Target A",
				"model":        "gpt-test",
				"protocol":     "openai_chat",
				"channel_id":   1,
			}
			delete(target, tt.field)
			encoded, err := common.Marshal([]map[string]any{target})
			require.NoError(t, err)

			_, err = loadWithEnvironment(map[string]string{envTargets: string(encoded)})
			require.Error(t, err)
		})
	}
}

func TestLoadAcceptsAndNormalizesSupportedProtocols(t *testing.T) {
	protocols := []Protocol{
		ProtocolOpenAIChat,
		ProtocolOpenAIResponses,
		ProtocolAnthropicMessages,
		ProtocolGeminiGenerateContent,
	}

	for _, protocol := range protocols {
		t.Run(string(protocol), func(t *testing.T) {
			target := map[string]any{
				"key":          "target-a",
				"group":        "default",
				"display_name": "Target A",
				"model":        "gpt-test",
				"protocol":     " \t" + string(protocol) + "\r\n ",
				"channel_id":   1,
			}
			encoded, err := common.Marshal([]map[string]any{target})
			require.NoError(t, err)

			setting, err := loadWithEnvironment(map[string]string{envTargets: string(encoded)})
			require.NoError(t, err)
			require.Len(t, setting.Targets, 1)
			assert.Equal(t, protocol, setting.Targets[0].Protocol)
		})
	}
}

func TestLoadRejectsInvalidProtocolsWithoutEchoingValues(t *testing.T) {
	const marker = "secret-unknown-protocol-marker"
	tests := []struct {
		name    string
		targets string
	}{
		{
			name:    "missing",
			targets: `[{"key":"a","group":"a","display_name":"A","model":"m","channel_id":1}]`,
		},
		{
			name:    "unknown",
			targets: `[{"key":"a","group":"a","display_name":"A","model":"m","protocol":"secret-unknown-protocol-marker","channel_id":1}]`,
		},
		{
			name:    "wrong case",
			targets: `[{"key":"a","group":"a","display_name":"A","model":"m","protocol":"OpenAI_Chat","channel_id":1}]`,
		},
		{
			name:    "trimmed empty",
			targets: `[{"key":"a","group":"a","display_name":"A","model":"m","protocol":" \t\r\n ","channel_id":1}]`,
		},
		{
			name:    "invalid utf8",
			targets: "[{\"key\":\"a\",\"group\":\"a\",\"display_name\":\"A\",\"model\":\"m\",\"protocol\":\"openai_\xffchat\",\"channel_id\":1}]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnvironment(map[string]string{envTargets: tt.targets})
			require.Error(t, err)
			assert.Equal(t, invalidConfigurationErrorText, err.Error())
			assert.NotContains(t, err.Error(), marker)
		})
	}
}

func TestLoadNormalizesTargetsAndKeyIndex(t *testing.T) {
	setting, err := loadWithEnvironment(map[string]string{envTargets: `[
		{"key":"  target-a  ","group":"  group-a  ","display_name":"  Target A  ","model":"  gpt-test  ","protocol":"  openai_chat  ","channel_id":1},
		{"key":"target-b","group":"group-b","display_name":"Target B","model":"gpt-test","protocol":"openai_responses","channel_id":2,"key_index":0},
		{"key":"target-c","group":"group-c","display_name":"Target C","model":"gpt-test","protocol":"anthropic_messages","channel_id":3,"key_index":2}
	]`})
	require.NoError(t, err)
	require.Len(t, setting.Targets, 3)
	assert.Equal(t, Target{
		Enabled:     true,
		Key:         "target-a",
		Group:       "group-a",
		DisplayName: "Target A",
		Model:       "gpt-test",
		Protocol:    "openai_chat",
		ChannelID:   1,
		KeyIndex:    0,
	}, setting.Targets[0])
	assert.Zero(t, setting.Targets[1].KeyIndex)
	assert.Equal(t, 2, setting.Targets[2].KeyIndex)
}

func TestLoadRejectsInvalidTargetSelectors(t *testing.T) {
	tests := []struct {
		name    string
		targets string
	}{
		{name: "duplicate stable key", targets: `[
			{"key":"same","group":"a","display_name":"A","model":"m","protocol":"openai_chat","channel_id":1},
			{"key":" same ","group":"b","display_name":"B","model":"m","protocol":"openai_chat","channel_id":2}
		]`},
		{name: "zero channel id", targets: `[{"key":"a","group":"a","display_name":"A","model":"m","protocol":"openai_chat","channel_id":0}]`},
		{name: "negative channel id", targets: `[{"key":"a","group":"a","display_name":"A","model":"m","protocol":"openai_chat","channel_id":-1}]`},
		{name: "negative key index", targets: `[{"key":"a","group":"a","display_name":"A","model":"m","protocol":"openai_chat","channel_id":1,"key_index":-1}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnvironment(map[string]string{envTargets: tt.targets})
			require.Error(t, err)
		})
	}
}

func TestLoadRejectsUnknownTargetFieldsWithoutEchoingValues(t *testing.T) {
	const marker = "unknown-field-secret-marker"
	tests := []struct {
		name    string
		targets string
	}{
		{
			name:    "unknown field",
			targets: `[{"key":"a","group":"a","display_name":"A","model":"m","protocol":"openai_chat","channel_id":1,"unexpected":"unknown-field-secret-marker"}]`,
		},
		{
			name:    "misspelled key index",
			targets: `[{"key":"a","group":"a","display_name":"A","model":"m","protocol":"openai_chat","channel_id":1,"key_indxe":"unknown-field-secret-marker"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnvironment(map[string]string{envTargets: tt.targets})
			require.Error(t, err)
			assert.Equal(t, invalidConfigurationErrorText, err.Error())
			assert.NotContains(t, err.Error(), marker)
		})
	}
}

func TestLoadRejectsOversizedTargetStrings(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "key", field: "key", value: strings.Repeat("k", maxKeyRunes+1)},
		{name: "group", field: "group", value: strings.Repeat("g", maxGroupRunes+1)},
		{name: "display name", field: "display_name", value: strings.Repeat("d", maxDisplayNameRunes+1)},
		{name: "model", field: "model", value: strings.Repeat("m", maxModelRunes+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := map[string]any{
				"key":          "target-a",
				"group":        "default",
				"display_name": "Target A",
				"model":        "gpt-test",
				"protocol":     "openai_chat",
				"channel_id":   1,
			}
			target[tt.field] = tt.value
			encoded, err := common.Marshal([]map[string]any{target})
			require.NoError(t, err)

			_, err = loadWithEnvironment(map[string]string{envTargets: string(encoded)})
			require.Error(t, err)
		})
	}
}

func TestLoadAcceptsTargetStringsAtRuneLimits(t *testing.T) {
	target := map[string]any{
		"key":          strings.Repeat("k", maxKeyRunes),
		"group":        strings.Repeat("g", maxGroupRunes),
		"display_name": strings.Repeat("d", maxDisplayNameRunes),
		"model":        strings.Repeat("m", maxModelRunes),
		"protocol":     "openai_chat",
		"channel_id":   1,
	}
	encoded, err := common.Marshal([]map[string]any{target})
	require.NoError(t, err)

	setting, err := loadWithEnvironment(map[string]string{envTargets: string(encoded)})
	require.NoError(t, err)
	assert.Len(t, setting.Targets, 1)
}

func TestLoadRejectsInvalidUTF8(t *testing.T) {
	values := map[string]string{
		envTargets: "[{\"key\":\"target-a\",\"group\":\"default\",\"display_name\":\"\xff\",\"model\":\"gpt-test\",\"protocol\":\"openai_chat\",\"channel_id\":1}]",
	}

	_, err := loadWithEnvironment(values)
	require.Error(t, err)
}

func TestLoadValidatesTargetJSONSizeLimit(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "exact limit", size: maxTargetsJSONBytes},
		{name: "over limit", size: maxTargetsJSONBytes + 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithEnvironment(map[string]string{envTargets: "[]" + strings.Repeat(" ", tt.size-2)})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLoadValidatesExplicitTargetsWhileDisabled(t *testing.T) {
	_, err := loadWithEnvironment(map[string]string{
		envEnabled: "false",
		envTargets: `[{"key":"invalid"}]`,
	})
	require.Error(t, err)
}

func TestLoadReturnsFreshTargetSlices(t *testing.T) {
	values := map[string]string{envTargets: validTargetJSON}

	first, err := loadWithEnvironment(values)
	require.NoError(t, err)
	first.Targets[0].Key = "mutated"

	second, err := loadWithEnvironment(values)
	require.NoError(t, err)
	assert.Equal(t, "target-a", second.Targets[0].Key)
}

func TestLoadErrorsNeverEchoConfiguration(t *testing.T) {
	const marker = "secret-marker"
	_, err := loadWithEnvironment(map[string]string{envTargets: `[
		{"key":"secret-marker","group":"a","display_name":"A","model":"m","protocol":"openai_chat","channel_id":1},
		{"key":"secret-marker","group":"b","display_name":"B","model":"m","protocol":"openai_chat","channel_id":2}
	]`})
	require.Error(t, err)
	assert.Equal(t, "invalid public status probe configuration", err.Error())
	assert.NotContains(t, err.Error(), marker)
	assert.LessOrEqual(t, len(err.Error()), 64)
}
