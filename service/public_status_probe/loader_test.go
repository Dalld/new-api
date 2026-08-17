package public_status_probe

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newProbeLoaderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func insertProbeChannel(t *testing.T, db *gorm.DB, channel model.Channel) model.Channel {
	t.Helper()
	if channel.Status == 0 {
		channel.Status = common.ChannelStatusEnabled
	}
	if channel.Key == "" {
		channel.Key = "provider-key"
	}
	if channel.Models == "" {
		channel.Models = "public-model"
	}
	if channel.BaseURL == nil {
		baseURL := "https://provider.example"
		channel.BaseURL = &baseURL
	}
	require.NoError(t, db.Create(&channel).Error)
	return channel
}

func TestDBTargetLoaderRejectsModelNotConfiguredOnChannel(t *testing.T) {
	db := newProbeLoaderTestDB(t)
	channel := insertProbeChannel(t, db, model.Channel{
		Id:     41,
		Type:   constant.ChannelTypeOpenAI,
		Models: "other-model, another-model ",
	})

	_, err := NewDBTargetLoader(db).Load(
		context.Background(),
		probeTarget(channel.Id, publicstatusprobesetting.ProtocolOpenAIChat),
	)

	require.Error(t, err)
	assert.Equal(t, ErrorInvalidTarget, ErrorCodeOf(err))
}

func probeTarget(channelID int, protocol publicstatusprobesetting.Protocol) publicstatusprobesetting.Target {
	return publicstatusprobesetting.Target{
		Key:         "target-a",
		Group:       "default",
		DisplayName: "Target A",
		Model:       "public-model",
		Protocol:    protocol,
		ChannelID:   channelID,
	}
}

func TestDBTargetLoaderSupportsOnlyExplicitNativeProtocols(t *testing.T) {
	tests := []struct {
		name         string
		channelType  int
		protocol     publicstatusprobesetting.Protocol
		wantProtocol Protocol
	}{
		{name: "OpenAI Chat", channelType: constant.ChannelTypeOpenAI, protocol: publicstatusprobesetting.ProtocolOpenAIChat, wantProtocol: ProtocolOpenAIChat},
		{name: "OpenAI Responses", channelType: constant.ChannelTypeOpenAI, protocol: publicstatusprobesetting.ProtocolOpenAIResponses, wantProtocol: ProtocolOpenAIResponses},
		{name: "Anthropic Messages", channelType: constant.ChannelTypeAnthropic, protocol: publicstatusprobesetting.ProtocolAnthropicMessages, wantProtocol: ProtocolAnthropicMessages},
		{name: "Gemini GenerateContent", channelType: constant.ChannelTypeGemini, protocol: publicstatusprobesetting.ProtocolGeminiGenerateContent, wantProtocol: ProtocolGeminiGenerateContent},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newProbeLoaderTestDB(t)
			organization := "org-id"
			channel := insertProbeChannel(t, db, model.Channel{
				Id:                 index + 1,
				Type:               test.channelType,
				OpenAIOrganization: &organization,
			})

			loaded, err := NewDBTargetLoader(db).Load(context.Background(), probeTarget(channel.Id, test.protocol))

			require.NoError(t, err)
			assert.Equal(t, test.wantProtocol, loaded.Snapshot.Protocol)
			assert.Equal(t, "provider-key", loaded.Snapshot.APIKey)
			assert.Equal(t, "public-model", loaded.Snapshot.Model)
			if test.wantProtocol == ProtocolOpenAIChat || test.wantProtocol == ProtocolOpenAIResponses {
				assert.Equal(t, organization, loaded.Snapshot.Organization)
			} else {
				assert.Empty(t, loaded.Snapshot.Organization)
			}
		})
	}
}

func TestDBTargetLoaderMapsModelAndSelectsExactEnabledKeyWithoutMutation(t *testing.T) {
	db := newProbeLoaderTestDB(t)
	mapping := `{"public-model":"middle-model","middle-model":"upstream-model"}`
	channel := insertProbeChannel(t, db, model.Channel{
		Id:           42,
		Type:         constant.ChannelTypeOpenAI,
		Key:          "first-key\nsecond-key\nthird-key",
		ModelMapping: &mapping,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         3,
			MultiKeyStatusList:   map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled, 2: common.ChannelStatusManuallyDisabled},
			MultiKeyPollingIndex: 2,
		},
	})
	var before model.Channel
	require.NoError(t, db.First(&before, channel.Id).Error)
	target := probeTarget(channel.Id, publicstatusprobesetting.ProtocolOpenAIResponses)
	target.KeyIndex = 1

	loaded, err := NewDBTargetLoader(db).Load(context.Background(), target)

	require.NoError(t, err)
	assert.Equal(t, "second-key", loaded.Snapshot.APIKey)
	assert.Equal(t, "upstream-model", loaded.Snapshot.Model)
	var after model.Channel
	require.NoError(t, db.First(&after, channel.Id).Error)
	assert.Equal(t, before.Status, after.Status)
	assert.Equal(t, before.TestTime, after.TestTime)
	assert.Equal(t, before.ResponseTime, after.ResponseTime)
	assert.Equal(t, before.Key, after.Key)
	assert.Equal(t, before.ChannelInfo, after.ChannelInfo)
}

func TestDBTargetLoaderRejectsMissingDisabledAndInvalidKeyTargets(t *testing.T) {
	tests := []struct {
		name      string
		channel   model.Channel
		channelID int
		keyIndex  int
		wantCode  ErrorCode
	}{
		{name: "missing", channelID: 404, wantCode: ErrorInvalidTarget},
		{name: "disabled channel", channel: model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled}, channelID: 1, wantCode: ErrorInvalidTarget},
		{name: "single key index out of range", channel: model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI}, channelID: 1, keyIndex: 1, wantCode: ErrorInvalidTarget},
		{name: "multi key index out of range", channel: model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Key: "a\nb", ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}}, channelID: 1, keyIndex: 2, wantCode: ErrorInvalidTarget},
		{name: "explicit disabled key", channel: model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Key: "a\nb", ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyStatusList: map[int]int{1: common.ChannelStatusManuallyDisabled}}}, channelID: 1, keyIndex: 1, wantCode: ErrorInvalidTarget},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newProbeLoaderTestDB(t)
			if test.channel.Id != 0 {
				insertProbeChannel(t, db, test.channel)
			}
			target := probeTarget(test.channelID, publicstatusprobesetting.ProtocolOpenAIChat)
			target.KeyIndex = test.keyIndex

			_, err := NewDBTargetLoader(db).Load(context.Background(), target)

			require.Error(t, err)
			assert.Equal(t, test.wantCode, ErrorCodeOf(err))
		})
	}
}

func TestDBTargetLoaderRejectsProtocolMismatchAndSpecialChannelBehavior(t *testing.T) {
	proxySetting := `{"proxy":"http://proxy.example:8080"}`
	headerOverride := `{"X-Auth":"secret"}`
	paramOverride := `{"temperature":1}`
	tests := []struct {
		name     string
		channel  model.Channel
		protocol publicstatusprobesetting.Protocol
	}{
		{name: "protocol mismatch", channel: model.Channel{Id: 1, Type: constant.ChannelTypeAnthropic}, protocol: publicstatusprobesetting.ProtocolOpenAIChat},
		{name: "Codex OAuth", channel: model.Channel{Id: 1, Type: constant.ChannelTypeCodex}, protocol: publicstatusprobesetting.ProtocolOpenAIResponses},
		{name: "proxy", channel: model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Setting: &proxySetting}, protocol: publicstatusprobesetting.ProtocolOpenAIChat},
		{name: "header override", channel: model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, HeaderOverride: &headerOverride}, protocol: publicstatusprobesetting.ProtocolOpenAIChat},
		{name: "parameter override", channel: model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, ParamOverride: &paramOverride}, protocol: publicstatusprobesetting.ProtocolOpenAIChat},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newProbeLoaderTestDB(t)
			channel := insertProbeChannel(t, db, test.channel)

			_, err := NewDBTargetLoader(db).Load(context.Background(), probeTarget(channel.Id, test.protocol))

			require.Error(t, err)
			assert.Equal(t, ErrorUnsupportedProvider, ErrorCodeOf(err))
			assert.Equal(t, string(ErrorUnsupportedProvider), err.Error())
		})
	}
}

func TestMapProbeModelRejectsMalformedAndCyclicMappings(t *testing.T) {
	for _, mapping := range []string{
		`{"public-model":`,
		`{"public-model":"middle","middle":"public-model"}`,
		strings.Repeat("x", maxModelMappingBytes+1),
	} {
		mapped, err := mapProbeModel("public-model", &mapping)
		require.Error(t, err)
		assert.Empty(t, mapped)
		assert.Equal(t, ErrorInvalidTarget, ErrorCodeOf(err))
	}
}

func TestValidateTargetDoesNotMutateChannel(t *testing.T) {
	db := newProbeLoaderTestDB(t)
	mapping := `{"public-model":"upstream-model"}`
	channel := insertProbeChannel(t, db, model.Channel{
		Id:           99,
		Type:         constant.ChannelTypeOpenAI,
		Key:          "first-key\nsecond-key",
		ModelMapping: &mapping,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyStatusList:   map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled},
			MultiKeyPollingIndex: 1,
		},
	})
	var before model.Channel
	require.NoError(t, db.First(&before, channel.Id).Error)

	target := probeTarget(channel.Id, publicstatusprobesetting.ProtocolOpenAIChat)
	target.KeyIndex = 1

	require.NoError(t, NewDBTargetLoader(db).ValidateTarget(context.Background(), target))

	var after model.Channel
	require.NoError(t, db.First(&after, channel.Id).Error)
	assert.Equal(t, before, after)
}
