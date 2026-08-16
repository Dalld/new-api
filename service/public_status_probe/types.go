package public_status_probe

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	DefaultPingTimeout         = 8 * time.Second
	DefaultConversationTimeout = 45 * time.Second
	DefaultDegradedThreshold   = 6 * time.Second
	MaxResponseBytes           = 1 << 20
)

type Protocol uint8

const (
	ProtocolUnknown Protocol = iota
	ProtocolOpenAIChat
	ProtocolOpenAIResponses
	ProtocolAnthropicMessages
	ProtocolGeminiGenerateContent
)

type Snapshot struct {
	Protocol     Protocol
	BaseURL      string
	APIKey       string
	Model        string
	Organization string
}

type Challenge struct {
	Expected string
	Prompt   string
}

type Adapter interface {
	Probe(context.Context, Snapshot, Challenge) (string, error)
}

type ErrorCode string

const (
	ErrorUnsupportedProvider ErrorCode = "unsupported_provider"
	ErrorTimeout             ErrorCode = "timeout"
	ErrorNetwork             ErrorCode = "network_error"
	ErrorProviderRejected    ErrorCode = "provider_rejected"
	ErrorEmptyResponse       ErrorCode = "empty_response"
	ErrorValidationFailed    ErrorCode = "validation_failed"
	ErrorInvalidTarget       ErrorCode = "invalid_target"
	ErrorResponseTooLarge    ErrorCode = "response_too_large"
)

// ProbeError intentionally exposes only a stable code.
type ProbeError struct {
	Code ErrorCode
}

func (e *ProbeError) Error() string {
	if e == nil {
		return ""
	}
	return string(stableErrorCode(e.Code))
}

func codedError(code ErrorCode) error {
	return &ProbeError{Code: code}
}

func stableErrorCode(code ErrorCode) ErrorCode {
	switch code {
	case ErrorUnsupportedProvider,
		ErrorTimeout,
		ErrorNetwork,
		ErrorProviderRejected,
		ErrorEmptyResponse,
		ErrorValidationFailed,
		ErrorInvalidTarget,
		ErrorResponseTooLarge:
		return code
	default:
		return ErrorNetwork
	}
}

func ErrorCodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var probeErr *ProbeError
	if errors.As(err, &probeErr) && probeErr != nil {
		return stableErrorCode(probeErr.Code)
	}
	return ErrorNetwork
}

type ProbeState string

const (
	StateUnknown          ProbeState = "unknown"
	StateOperational      ProbeState = "operational"
	StateDegraded         ProbeState = "degraded"
	StateValidationFailed ProbeState = "validation_failed"
	StateFailed           ProbeState = "failed"
)

type PingResult struct {
	Reachable bool
	LatencyMS *int64
	Code      ErrorCode
}

type ConversationResult struct {
	State     ProbeState
	LatencyMS *int64
	Code      ErrorCode
}

type ProbeResult struct {
	State         ProbeState
	PingReachable bool
	PingLatencyMS *int64
	ChatLatencyMS *int64
	ErrorCode     ErrorCode
	PingErrorCode ErrorCode
}

func CombineResults(ping PingResult, conversation ConversationResult) ProbeResult {
	return ProbeResult{
		State:         conversation.State,
		PingReachable: ping.Reachable,
		PingLatencyMS: ping.LatencyMS,
		ChatLatencyMS: conversation.LatencyMS,
		ErrorCode:     conversation.Code,
		PingErrorCode: ping.Code,
	}
}

func normalizedOutput(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
