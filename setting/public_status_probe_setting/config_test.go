package public_status_probe_setting

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validDocument() Document {
	return Document{
		SchemaVersion:      SchemaVersion,
		Version:            2,
		Enabled:            true,
		PingTimeoutSeconds: 8,
		ChatTimeoutSeconds: 45,
		DegradedLatencyMS:  6000,
		Concurrency:        5,
		RetentionDays:      7,
		Targets: []Target{
			{Enabled: true, Key: "target-b", Group: "group-b", DisplayName: "Target B", Model: "model-b", Protocol: ProtocolOpenAIResponses, ChannelID: 2, KeyIndex: 1},
			{Enabled: false, Key: "target-a", Group: "group-a", DisplayName: "Target A", Model: "model-a", Protocol: ProtocolOpenAIChat, ChannelID: 1, KeyIndex: 0},
		},
	}
}

func TestDefaultDocument(t *testing.T) {
	document := DefaultDocument()

	assert.Equal(t, 1, document.SchemaVersion)
	assert.Equal(t, int64(1), document.Version)
	assert.False(t, document.Enabled)
	assert.Equal(t, 8, document.PingTimeoutSeconds)
	assert.Equal(t, 45, document.ChatTimeoutSeconds)
	assert.Equal(t, 6000, document.DegradedLatencyMS)
	assert.Equal(t, 5, document.Concurrency)
	assert.Equal(t, 7, document.RetentionDays)
	assert.Empty(t, document.Targets)
	assert.NotNil(t, document.Targets)
	assert.Equal(t, "PublicStatusProbeConfig", OptionKey)
	assert.Equal(t, 1, SchemaVersion)
}

func TestDocumentRoundTripPreservesTargetOrder(t *testing.T) {
	want := validDocument()
	raw, err := EncodeDocument(want)
	require.NoError(t, err)

	got, err := DecodeDocument(raw)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Less(t, strings.Index(raw, `"key":"target-b"`), strings.Index(raw, `"key":"target-a"`))
}

func TestDecodeDocumentRejectsUnknownFields(t *testing.T) {
	tests := []string{
		`{"schema_version":1,"version":1,"enabled":false,"ping_timeout_seconds":8,"chat_timeout_seconds":45,"degraded_latency_ms":6000,"concurrency":5,"retention_days":7,"targets":[],"secret":"do-not-echo"}`,
		`{"schema_version":1,"version":1,"enabled":false,"ping_timeout_seconds":8,"chat_timeout_seconds":45,"degraded_latency_ms":6000,"concurrency":5,"retention_days":7,"targets":[{"enabled":true,"key":"a","group":"g","display_name":"A","model":"m","protocol":"openai_chat","channel_id":1,"key_index":0,"secret":"do-not-echo"}]}`,
	}

	for _, raw := range tests {
		_, err := DecodeDocument(raw)
		require.Error(t, err)
		assert.Equal(t, invalidConfigurationErrorText, err.Error())
		assert.NotContains(t, err.Error(), "do-not-echo")
		assert.LessOrEqual(t, len(err.Error()), 64)
	}
}

func TestDecodeDocumentRejectsInvalidSchemaVersionAndVersion(t *testing.T) {
	tests := []string{
		`{"schema_version":2,"version":1,"enabled":false,"ping_timeout_seconds":8,"chat_timeout_seconds":45,"degraded_latency_ms":6000,"concurrency":5,"retention_days":7,"targets":[]}`,
		`{"schema_version":1,"version":0,"enabled":false,"ping_timeout_seconds":8,"chat_timeout_seconds":45,"degraded_latency_ms":6000,"concurrency":5,"retention_days":7,"targets":[]}`,
		`{"schema_version":1,"version":-1,"enabled":false,"ping_timeout_seconds":8,"chat_timeout_seconds":45,"degraded_latency_ms":6000,"concurrency":5,"retention_days":7,"targets":[]}`,
	}

	for _, raw := range tests {
		_, err := DecodeDocument(raw)
		require.Error(t, err)
		assert.Equal(t, invalidConfigurationErrorText, err.Error())
	}
}

func TestDecodeDocumentRejectsNullScalarFields(t *testing.T) {
	raw, err := EncodeDocument(validDocument())
	require.NoError(t, err)

	tests := []struct {
		name string
		old  string
		new  string
	}{
		{name: "schema version", old: `"schema_version":1`, new: `"schema_version":null`},
		{name: "version", old: `"version":2`, new: `"version":null`},
		{name: "enabled", old: `"enabled":true`, new: `"enabled":null`},
		{name: "ping timeout", old: `"ping_timeout_seconds":8`, new: `"ping_timeout_seconds":null`},
		{name: "chat timeout", old: `"chat_timeout_seconds":45`, new: `"chat_timeout_seconds":null`},
		{name: "degraded latency", old: `"degraded_latency_ms":6000`, new: `"degraded_latency_ms":null`},
		{name: "concurrency", old: `"concurrency":5`, new: `"concurrency":null`},
		{name: "retention", old: `"retention_days":7`, new: `"retention_days":null`},
		{name: "target enabled", old: `"targets":[{"enabled":true`, new: `"targets":[{"enabled":null`},
		{name: "target key", old: `"key":"target-b"`, new: `"key":null`},
		{name: "target group", old: `"group":"group-b"`, new: `"group":null`},
		{name: "target display name", old: `"display_name":"Target B"`, new: `"display_name":null`},
		{name: "target model", old: `"model":"model-b"`, new: `"model":null`},
		{name: "target protocol", old: `"protocol":"openai_responses"`, new: `"protocol":null`},
		{name: "target channel id", old: `"channel_id":2`, new: `"channel_id":null`},
		{name: "target key index", old: `"key_index":1`, new: `"key_index":null`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withNull := strings.Replace(raw, tt.old, tt.new, 1)
			require.NotEqual(t, raw, withNull)
			_, err := DecodeDocument(withNull)
			require.Error(t, err)
			assert.Equal(t, invalidConfigurationErrorText, err.Error())
		})
	}
}

func TestValidateAndNormalizeDocumentScalarBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		valid   []int
		invalid []int
		set     func(*Document, int)
	}{
		{name: "ping timeout", valid: []int{1, 15}, invalid: []int{0, 16}, set: func(d *Document, v int) { d.PingTimeoutSeconds = v }},
		{name: "chat timeout", valid: []int{5, 60}, invalid: []int{4, 61}, set: func(d *Document, v int) { d.ChatTimeoutSeconds = v }},
		{name: "degraded latency", valid: []int{1, 60000}, invalid: []int{0, 60001}, set: func(d *Document, v int) { d.DegradedLatencyMS = v }},
		{name: "concurrency", valid: []int{1, 20}, invalid: []int{0, 21}, set: func(d *Document, v int) { d.Concurrency = v }},
		{name: "retention", valid: []int{1, 30}, invalid: []int{0, 31}, set: func(d *Document, v int) { d.RetentionDays = v }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, value := range tt.valid {
				document := validDocument()
				tt.set(&document, value)
				_, err := ValidateAndNormalizeDocument(document)
				require.NoError(t, err)
			}
			for _, value := range tt.invalid {
				document := validDocument()
				tt.set(&document, value)
				_, err := ValidateAndNormalizeDocument(document)
				require.Error(t, err)
			}
		})
	}
}

func TestValidateAndNormalizeDocumentTargets(t *testing.T) {
	document := validDocument()
	document.Targets[0].Key = " target-b "
	document.Targets[0].Group = " group-b "
	document.Targets[0].DisplayName = " Target B "
	document.Targets[0].Model = " model-b "
	document.Targets[0].Protocol = " openai_responses "

	normalized, err := ValidateAndNormalizeDocument(document)
	require.NoError(t, err)
	assert.Equal(t, "target-b", normalized.Targets[0].Key)
	assert.Equal(t, "group-b", normalized.Targets[0].Group)
	assert.Equal(t, "Target B", normalized.Targets[0].DisplayName)
	assert.Equal(t, "model-b", normalized.Targets[0].Model)
	assert.Equal(t, ProtocolOpenAIResponses, normalized.Targets[0].Protocol)
	assert.Equal(t, " target-b ", document.Targets[0].Key)
}

func TestValidateAndNormalizeDocumentRejectsInvalidTargets(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Document)
	}{
		{name: "too many", mutate: func(d *Document) {
			d.Targets = make([]Target, maxTargets+1)
			for i := range d.Targets {
				d.Targets[i] = validDocument().Targets[0]
				d.Targets[i].Key = string(rune('a' + i))
			}
		}},
		{name: "duplicate key", mutate: func(d *Document) { d.Targets[1].Key = " target-b " }},
		{name: "empty key", mutate: func(d *Document) { d.Targets[0].Key = " " }},
		{name: "long key", mutate: func(d *Document) { d.Targets[0].Key = strings.Repeat("k", maxKeyRunes+1) }},
		{name: "invalid protocol", mutate: func(d *Document) { d.Targets[0].Protocol = "secret-protocol" }},
		{name: "channel id", mutate: func(d *Document) { d.Targets[0].ChannelID = 0 }},
		{name: "key index", mutate: func(d *Document) { d.Targets[0].KeyIndex = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			document := validDocument()
			tt.mutate(&document)
			_, err := ValidateAndNormalizeDocument(document)
			require.Error(t, err)
			assert.Equal(t, invalidConfigurationErrorText, err.Error())
			assert.NotContains(t, err.Error(), "secret-protocol")
		})
	}
}

func TestDecodeDocumentRejectsInvalidUTF8AndOversizedInput(t *testing.T) {
	raw := `{"schema_version":1,"secret":"` + string([]byte{0xff}) + `"}`
	_, err := DecodeDocument(raw)
	require.Error(t, err)
	assert.Equal(t, invalidConfigurationErrorText, err.Error())

	_, err = DecodeDocument(strings.Repeat(" ", maxDocumentJSONBytes+1))
	require.Error(t, err)
	assert.Equal(t, invalidConfigurationErrorText, err.Error())
}

func TestPublishDocumentUsesImmutableSnapshotsAndCallsHook(t *testing.T) {
	require.NoError(t, PublishDocument(DefaultDocument()))
	t.Cleanup(func() {
		SetPublishHook(nil)
		require.NoError(t, PublishDocument(DefaultDocument()))
	})

	called := make(chan int64, 1)
	SetPublishHook(func(version int64) {
		current := CurrentDocument()
		assert.Equal(t, version, current.Version)
		called <- version
	})

	document := validDocument()
	require.NoError(t, PublishDocument(document))
	document.Targets[0].Key = "caller-mutated"

	first := CurrentDocument()
	assert.Equal(t, "target-b", first.Targets[0].Key)
	first.Targets[0].Key = "snapshot-mutated"
	assert.Equal(t, "target-b", CurrentDocument().Targets[0].Key)
	assert.Equal(t, int64(2), <-called)

	invalid := validDocument()
	invalid.Version = 3
	invalid.Targets[0].ChannelID = 0
	require.Error(t, PublishDocument(invalid))
	assert.Equal(t, int64(2), CurrentDocument().Version)
	select {
	case <-called:
		t.Fatal("publish hook called for invalid document")
	default:
	}
}

func TestCurrentSettingFiltersDisabledTargets(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, PublishDocument(DefaultDocument())) })
	require.NoError(t, PublishDocument(validDocument()))

	setting := CurrentSetting()
	assert.True(t, setting.Enabled)
	assert.Equal(t, time.Minute, setting.Interval)
	assert.Equal(t, 8*time.Second, setting.PingTimeout)
	assert.Equal(t, 45*time.Second, setting.ChatTimeout)
	assert.Equal(t, 6*time.Second, setting.DegradedLatency)
	assert.Equal(t, 5, setting.Concurrency)
	assert.Equal(t, 7, setting.RetentionDays)
	require.Len(t, setting.Targets, 1)
	assert.Equal(t, "target-b", setting.Targets[0].Key)

	setting.Targets[0].Key = "mutated"
	assert.Equal(t, "target-b", CurrentSetting().Targets[0].Key)
}

func TestLoadEnvironmentDocumentEnablesImportedTargets(t *testing.T) {
	document, err := loadEnvironmentDocument(func(name string) (string, bool) {
		values := map[string]string{
			envEnabled: "true",
			envTargets: `[
				{"key":"target-a","group":"default","display_name":"Target A","model":"gpt-test","protocol":"openai_chat","channel_id":1},
				{"enabled":false,"key":"target-b","group":"default","display_name":"Target B","model":"gpt-test","protocol":"openai_chat","channel_id":2}
			]`,
		}
		value, ok := values[name]
		return value, ok
	})
	require.NoError(t, err)
	require.Len(t, document.Targets, 2)
	assert.True(t, document.Targets[0].Enabled)
	assert.False(t, document.Targets[1].Enabled)
	assert.Equal(t, int64(1), document.Version)
	assert.Equal(t, SchemaVersion, document.SchemaVersion)
}
