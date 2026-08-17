package public_status_probe

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"gorm.io/gorm"
)

const maxModelMappingBytes = 64 * 1024

type LoadedTarget struct {
	TargetKey   string
	Group       string
	DisplayName string
	ModelName   string
	ChannelID   int
	Snapshot    Snapshot
}

type TargetLoader interface {
	Load(context.Context, publicstatusprobesetting.Target) (LoadedTarget, error)
}

type TargetValidator interface {
	ValidateTarget(context.Context, publicstatusprobesetting.Target) error
}

type DBTargetLoader struct {
	db *gorm.DB
}

type probeChannelRow struct {
	ID                 int
	Type               int
	Key                string
	OpenAIOrganization *string
	Status             int
	BaseURL            *string
	ModelMapping       *string
	Setting            *string
	ParamOverride      *string
	HeaderOverride     *string
	ChannelInfo        model.ChannelInfo
}

func NewDBTargetLoader(db *gorm.DB) *DBTargetLoader {
	return &DBTargetLoader{db: db}
}

func (loader *DBTargetLoader) Load(ctx context.Context, target publicstatusprobesetting.Target) (LoadedTarget, error) {
	if loader == nil || loader.db == nil {
		return LoadedTarget{}, codedError(ErrorInvalidTarget)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var channel probeChannelRow
	err := loader.db.WithContext(ctx).Table("channels").
		Select("id", "type", "key", "open_ai_organization", "status", "base_url", "model_mapping", "setting", "param_override", "header_override", "channel_info").
		Where("id = ?", target.ChannelID).
		Take(&channel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return LoadedTarget{}, codedError(ErrorInvalidTarget)
	}
	if err != nil {
		return LoadedTarget{}, codedError(ErrorNetwork)
	}
	if channel.ID != target.ChannelID || channel.Status != common.ChannelStatusEnabled {
		return LoadedTarget{}, codedError(ErrorInvalidTarget)
	}

	protocol, err := protocolForTarget(target.Protocol, channel.Type)
	if err != nil {
		return LoadedTarget{}, err
	}
	if hasConfiguredValue(channel.ParamOverride) || hasConfiguredValue(channel.HeaderOverride) || channelUsesProxy(channel.Setting) {
		return LoadedTarget{}, codedError(ErrorUnsupportedProvider)
	}

	apiKey, err := selectExplicitKey(channel.Key, channel.ChannelInfo, target.KeyIndex)
	if err != nil {
		return LoadedTarget{}, err
	}
	upstreamModel, err := mapProbeModel(target.Model, channel.ModelMapping)
	if err != nil {
		return LoadedTarget{}, err
	}

	baseURL := ""
	if channel.BaseURL != nil {
		baseURL = *channel.BaseURL
	}
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[channel.Type]
	}
	organization := ""
	if protocol == ProtocolOpenAIChat || protocol == ProtocolOpenAIResponses {
		if channel.OpenAIOrganization != nil {
			organization = *channel.OpenAIOrganization
		}
	}

	loaded := LoadedTarget{
		TargetKey:   target.Key,
		Group:       target.Group,
		DisplayName: target.DisplayName,
		ModelName:   target.Model,
		ChannelID:   target.ChannelID,
		Snapshot: Snapshot{
			Protocol:     protocol,
			BaseURL:      baseURL,
			APIKey:       apiKey,
			Model:        upstreamModel,
			Organization: organization,
		},
	}
	if _, err := validateSnapshot(loaded.Snapshot); err != nil {
		return LoadedTarget{}, err
	}
	return loaded, nil
}

func (loader *DBTargetLoader) ValidateTarget(ctx context.Context, target publicstatusprobesetting.Target) error {
	_, err := loader.Load(ctx, target)
	return err
}

func protocolForTarget(protocol publicstatusprobesetting.Protocol, channelType int) (Protocol, error) {
	switch protocol {
	case publicstatusprobesetting.ProtocolOpenAIChat:
		if channelType == constant.ChannelTypeOpenAI {
			return ProtocolOpenAIChat, nil
		}
	case publicstatusprobesetting.ProtocolOpenAIResponses:
		if channelType == constant.ChannelTypeOpenAI {
			return ProtocolOpenAIResponses, nil
		}
	case publicstatusprobesetting.ProtocolAnthropicMessages:
		if channelType == constant.ChannelTypeAnthropic {
			return ProtocolAnthropicMessages, nil
		}
	case publicstatusprobesetting.ProtocolGeminiGenerateContent:
		if channelType == constant.ChannelTypeGemini {
			return ProtocolGeminiGenerateContent, nil
		}
	}
	return ProtocolUnknown, codedError(ErrorUnsupportedProvider)
}

func hasConfiguredValue(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}

func channelUsesProxy(raw *string) bool {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return false
	}
	var setting dto.ChannelSettings
	if len(*raw) > maxModelMappingBytes || common.Unmarshal([]byte(*raw), &setting) != nil {
		return true
	}
	return strings.TrimSpace(setting.Proxy) != ""
}

func selectExplicitKey(raw string, info model.ChannelInfo, keyIndex int) (string, error) {
	if keyIndex < 0 {
		return "", codedError(ErrorInvalidTarget)
	}
	if !info.IsMultiKey {
		if keyIndex != 0 || raw == "" {
			return "", codedError(ErrorInvalidTarget)
		}
		return raw, nil
	}

	keys := strings.Split(strings.Trim(raw, "\n"), "\n")
	if keyIndex >= len(keys) || keys[keyIndex] == "" {
		return "", codedError(ErrorInvalidTarget)
	}
	if status, exists := info.MultiKeyStatusList[keyIndex]; exists && status != common.ChannelStatusEnabled {
		return "", codedError(ErrorInvalidTarget)
	}
	return keys[keyIndex], nil
}

func mapProbeModel(configured string, rawMapping *string) (string, error) {
	if rawMapping == nil || strings.TrimSpace(*rawMapping) == "" || strings.TrimSpace(*rawMapping) == "{}" {
		return configured, nil
	}
	if len(*rawMapping) > maxModelMappingBytes {
		return "", codedError(ErrorInvalidTarget)
	}
	modelMap := make(map[string]string)
	if err := common.Unmarshal([]byte(*rawMapping), &modelMap); err != nil {
		return "", codedError(ErrorInvalidTarget)
	}

	current := configured
	visited := map[string]struct{}{current: {}}
	for {
		mapped, exists := modelMap[current]
		if !exists || mapped == "" || mapped == current {
			return current, nil
		}
		if _, exists := visited[mapped]; exists {
			return "", codedError(ErrorInvalidTarget)
		}
		visited[mapped] = struct{}{}
		current = mapped
	}
}
