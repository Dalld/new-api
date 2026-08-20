package public_status_probe

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const adapterTestPrompt = "return the nonce"

func TestOpenAIChatResponsesAnthropicAndGeminiAdapterRequestContractsAndTextExtraction(t *testing.T) {
	tests := []struct {
		name              string
		protocol          Protocol
		baseSuffix        string
		model             string
		wantRequestURI    string
		wantAuthorization string
		wantGoogleAPIKey  string
		wantAnthropicKey  string
		wantAnthropicVer  string
		wantOrganization  string
		wantBody          string
		providerResponse  string
		wantOutput        string
	}{
		{
			name:              "OpenAI Chat",
			protocol:          ProtocolOpenAIChat,
			baseSuffix:        "/v1",
			model:             "chat-model",
			wantRequestURI:    "/v1/chat/completions",
			wantAuthorization: "Bearer provider-key",
			wantOrganization:  "org-id",
			wantBody:          `{"model":"chat-model","messages":[{"role":"user","content":"return the nonce"}],"max_tokens":24,"stream":false,"temperature":0}`,
			providerResponse:  `{"choices":[{"message":{"content":"psp_token"}}]}`,
			wantOutput:        "psp_token",
		},
		{
			name:              "OpenAI Responses",
			protocol:          ProtocolOpenAIResponses,
			baseSuffix:        "/gateway",
			model:             "responses-model",
			wantRequestURI:    "/gateway/v1/responses",
			wantAuthorization: "Bearer provider-key",
			wantOrganization:  "org-id",
			wantBody:          `{"model":"responses-model","input":"return the nonce","max_output_tokens":24,"stream":false}`,
			providerResponse:  `{"output":[{"content":[{"type":"output_text","text":"psp_"},{"type":"refusal","text":"ignored"},{"type":"output_text","text":"token"}]}]}`,
			wantOutput:        "psp_token",
		},
		{
			name:             "Anthropic Messages",
			protocol:         ProtocolAnthropicMessages,
			model:            "claude-model",
			wantRequestURI:   "/v1/messages",
			wantAnthropicKey: "provider-key",
			wantAnthropicVer: "2023-06-01",
			wantBody:         `{"model":"claude-model","max_tokens":24,"messages":[{"role":"user","content":"return the nonce"}]}`,
			providerResponse: `{"content":[{"type":"text","text":"psp_"},{"type":"tool_use"},{"type":"text","text":"token"}]}`,
			wantOutput:       "psp_token",
		},
		{
			name:             "Gemini GenerateContent",
			protocol:         ProtocolGeminiGenerateContent,
			baseSuffix:       "/v1beta",
			model:            "gemini/pro",
			wantRequestURI:   "/v1beta/models/gemini%2Fpro:generateContent",
			wantGoogleAPIKey: "provider-key",
			wantBody:         `{"contents":[{"parts":[{"text":"return the nonce"}]}],"generationConfig":{"maxOutputTokens":24,"temperature":0}}`,
			providerResponse: `{"candidates":[{"content":{"parts":[{"text":"psp_"},{"text":"token"}]}}]}`,
			wantOutput:       "psp_token",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requestCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				requestCount.Add(1)
				assert.Equal(t, http.MethodPost, request.Method)
				assert.Equal(t, test.wantRequestURI, request.RequestURI)
				assert.Empty(t, request.URL.RawQuery)
				assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
				assert.Equal(t, test.wantAuthorization, request.Header.Get("Authorization"))
				assert.Equal(t, test.wantGoogleAPIKey, request.Header.Get("x-goog-api-key"))
				assert.Equal(t, test.wantAnthropicKey, request.Header.Get("x-api-key"))
				assert.Equal(t, test.wantAnthropicVer, request.Header.Get("anthropic-version"))
				assert.Equal(t, test.wantOrganization, request.Header.Get("OpenAI-Organization"))
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				assert.JSONEq(t, test.wantBody, string(body))
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.providerResponse)
			}))
			t.Cleanup(server.Close)

			snapshot := validAdapterSnapshot(test.protocol, server.URL+test.baseSuffix)
			snapshot.Model = test.model
			adapter, err := AdapterFor(snapshot, server.Client())
			require.NoError(t, err)
			output, err := adapter.Probe(context.Background(), snapshot, Challenge{Prompt: adapterTestPrompt})

			require.NoError(t, err)
			assert.Equal(t, test.wantOutput, output)
			assert.EqualValues(t, 1, requestCount.Load())
		})
	}
}

func TestOpenAIResponsesUsesTopLevelOutputText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"output_text":"psp_top_level","output":[{"content":[{"type":"output_text","text":"ignored"}]}]}`)
	}))
	t.Cleanup(server.Close)
	snapshot := validAdapterSnapshot(ProtocolOpenAIResponses, server.URL)
	adapter, err := AdapterFor(snapshot, server.Client())
	require.NoError(t, err)

	output, err := adapter.Probe(context.Background(), snapshot, Challenge{Prompt: adapterTestPrompt})

	require.NoError(t, err)
	assert.Equal(t, "psp_top_level", output)
}

func TestAdapterForRejectsUnsupportedProtocolWithoutFallback(t *testing.T) {
	adapter, err := AdapterFor(Snapshot{Protocol: ProtocolUnknown}, nil)

	require.Error(t, err)
	assert.Nil(t, adapter)
	assert.Equal(t, ErrorUnsupportedProvider, ErrorCodeOf(err))
	assert.Equal(t, string(ErrorUnsupportedProvider), err.Error())
}

func TestProviderAdaptersStrictlyValidateSnapshots(t *testing.T) {
	invalidUTF8 := string([]byte{0xff})
	valid := validAdapterSnapshot(ProtocolOpenAIChat, "https://example.com")
	tests := []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{name: "missing base URL", mutate: func(snapshot *Snapshot) { snapshot.BaseURL = "" }},
		{name: "overlong base URL", mutate: func(snapshot *Snapshot) {
			snapshot.BaseURL = "https://example.com/" + strings.Repeat("a", maxBaseURLBytes)
		}},
		{name: "non HTTP scheme", mutate: func(snapshot *Snapshot) { snapshot.BaseURL = "ftp://example.com" }},
		{name: "missing host", mutate: func(snapshot *Snapshot) { snapshot.BaseURL = "https:///v1" }},
		{name: "userinfo", mutate: func(snapshot *Snapshot) { snapshot.BaseURL = "https://user:password@example.com/v1" }},
		{name: "query", mutate: func(snapshot *Snapshot) { snapshot.BaseURL = "https://example.com/v1?key=secret-value" }},
		{name: "fragment", mutate: func(snapshot *Snapshot) { snapshot.BaseURL = "https://example.com/v1#secret-value" }},
		{name: "zero port", mutate: func(snapshot *Snapshot) { snapshot.BaseURL = "https://example.com:0/v1" }},
		{name: "path traversal", mutate: func(snapshot *Snapshot) { snapshot.BaseURL = "https://example.com/api/../v1" }},
		{name: "encoded path", mutate: func(snapshot *Snapshot) { snapshot.BaseURL = "https://example.com/api/%2e%2e/v1" }},
		{name: "missing API key", mutate: func(snapshot *Snapshot) { snapshot.APIKey = "" }},
		{name: "blank API key", mutate: func(snapshot *Snapshot) { snapshot.APIKey = " \t " }},
		{name: "API key leading whitespace", mutate: func(snapshot *Snapshot) { snapshot.APIKey = " provider-key" }},
		{name: "API key trailing whitespace", mutate: func(snapshot *Snapshot) { snapshot.APIKey = "provider-key " }},
		{name: "invalid UTF8 API key", mutate: func(snapshot *Snapshot) { snapshot.APIKey = invalidUTF8 }},
		{name: "overlong API key", mutate: func(snapshot *Snapshot) { snapshot.APIKey = strings.Repeat("k", maxAPIKeyBytes+1) }},
		{name: "API key CRLF", mutate: func(snapshot *Snapshot) { snapshot.APIKey = "key\r\nX-Leak: value" }},
		{name: "missing model", mutate: func(snapshot *Snapshot) { snapshot.Model = "" }},
		{name: "blank model", mutate: func(snapshot *Snapshot) { snapshot.Model = " \t " }},
		{name: "model leading whitespace", mutate: func(snapshot *Snapshot) { snapshot.Model = " provider-model" }},
		{name: "model trailing whitespace", mutate: func(snapshot *Snapshot) { snapshot.Model = "provider-model " }},
		{name: "invalid UTF8 model", mutate: func(snapshot *Snapshot) { snapshot.Model = invalidUTF8 }},
		{name: "overlong model", mutate: func(snapshot *Snapshot) { snapshot.Model = strings.Repeat("m", maxModelBytes+1) }},
		{name: "organization leading whitespace", mutate: func(snapshot *Snapshot) { snapshot.Organization = " org-id" }},
		{name: "organization trailing whitespace", mutate: func(snapshot *Snapshot) { snapshot.Organization = "org-id " }},
		{name: "invalid UTF8 organization", mutate: func(snapshot *Snapshot) { snapshot.Organization = invalidUTF8 }},
		{name: "overlong organization", mutate: func(snapshot *Snapshot) { snapshot.Organization = strings.Repeat("o", maxOrganizationBytes+1) }},
		{name: "organization CRLF", mutate: func(snapshot *Snapshot) { snapshot.Organization = "org\r\nX-Leak: value" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := valid
			test.mutate(&snapshot)
			adapter, err := AdapterFor(snapshot, nil)

			require.Error(t, err)
			assert.Nil(t, adapter)
			assert.Equal(t, ErrorInvalidTarget, ErrorCodeOf(err))
			assert.Equal(t, string(ErrorInvalidTarget), err.Error())
			assert.NotContains(t, err.Error(), "secret-value")
			assert.NotContains(t, err.Error(), "password")
			assert.True(t, utf8.ValidString(err.Error()))
		})
	}
}

func TestProviderAdaptersAllowEmptyOrganization(t *testing.T) {
	snapshot := validAdapterSnapshot(ProtocolOpenAIChat, "https://example.com")
	snapshot.Organization = ""

	adapter, err := AdapterFor(snapshot, nil)

	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

func TestProviderAdapterProbeRevalidatesSnapshot(t *testing.T) {
	snapshot := validAdapterSnapshot(ProtocolOpenAIChat, "https://example.com")
	adapter, err := AdapterFor(snapshot, nil)
	require.NoError(t, err)
	snapshot.APIKey = ""

	_, err = adapter.Probe(context.Background(), snapshot, Challenge{Prompt: adapterTestPrompt})

	require.Error(t, err)
	assert.Equal(t, ErrorInvalidTarget, ErrorCodeOf(err))
}

func TestProviderAdapterResponseErrorsAreStableAndSanitized(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		response string
		wantCode ErrorCode
	}{
		{name: "malformed JSON", status: http.StatusOK, response: `{"credential":"provider-secret"`, wantCode: ErrorValidationFailed},
		{name: "missing structure", status: http.StatusOK, response: `{}`, wantCode: ErrorValidationFailed},
		{name: "provider rejected", status: http.StatusUnauthorized, response: `credential=provider-secret`, wantCode: ErrorProviderRejected},
	}
	for _, protocol := range adapterProtocols() {
		for _, test := range tests {
			t.Run(protocolName(protocol)+"/"+test.name, func(t *testing.T) {
				wantCode := test.wantCode
				if protocol == ProtocolAnthropicMessages && test.name == "missing structure" {
					wantCode = ErrorEmptyResponse
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(test.status)
					_, _ = io.WriteString(w, test.response)
				}))
				t.Cleanup(server.Close)
				snapshot := validAdapterSnapshot(protocol, server.URL)
				adapter, err := AdapterFor(snapshot, server.Client())
				require.NoError(t, err)

				_, err = adapter.Probe(context.Background(), snapshot, Challenge{Prompt: adapterTestPrompt})

				require.Error(t, err)
				assert.Equal(t, wantCode, ErrorCodeOf(err))
				assert.Equal(t, string(wantCode), err.Error())
				assert.NotContains(t, err.Error(), "provider-secret")
			})
		}
	}
}

func TestAnthropicAdapterClassifiesStructuredErrorsAndNonTextResponses(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantCode ErrorCode
	}{
		{
			name:     "structured error",
			response: `{"type":"error","error":{"type":"invalid_request_error"}}`,
			wantCode: ErrorProviderRejected,
		},
		{
			name:     "thinking without text",
			response: `{"type":"message","content":[{"type":"thinking","thinking":"internal"}]}`,
			wantCode: ErrorEmptyResponse,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, test.response)
			}))
			t.Cleanup(server.Close)

			snapshot := validAdapterSnapshot(ProtocolAnthropicMessages, server.URL)
			adapter, err := AdapterFor(snapshot, server.Client())
			require.NoError(t, err)

			_, err = adapter.Probe(context.Background(), snapshot, Challenge{Prompt: adapterTestPrompt})

			require.Error(t, err)
			assert.Equal(t, test.wantCode, ErrorCodeOf(err))
		})
	}
}

func TestProviderAdapterEmptyTextIsStable(t *testing.T) {
	tests := []struct {
		name     string
		protocol Protocol
		response string
	}{
		{name: "OpenAI Chat", protocol: ProtocolOpenAIChat, response: `{"choices":[{"message":{"content":" \n"}}]}`},
		{name: "OpenAI Responses", protocol: ProtocolOpenAIResponses, response: `{"output_text":" \n"}`},
		{name: "Anthropic", protocol: ProtocolAnthropicMessages, response: `{"content":[{"type":"text","text":" \n"}]}`},
		{name: "Gemini", protocol: ProtocolGeminiGenerateContent, response: `{"candidates":[{"content":{"parts":[{"text":" \n"}]}}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, test.response)
			}))
			t.Cleanup(server.Close)
			snapshot := validAdapterSnapshot(test.protocol, server.URL)
			adapter, err := AdapterFor(snapshot, server.Client())
			require.NoError(t, err)

			_, err = adapter.Probe(context.Background(), snapshot, Challenge{Prompt: adapterTestPrompt})

			require.Error(t, err)
			assert.Equal(t, ErrorEmptyResponse, ErrorCodeOf(err))
			assert.Equal(t, string(ErrorEmptyResponse), err.Error())
		})
	}
}

func TestOpenAIChatResponsesAnthropicAndGeminiAdaptersRejectOversizedHTTPResponseBodies(t *testing.T) {
	const marker = "oversized-response-secret-marker"
	for _, protocol := range adapterProtocols() {
		t.Run(protocolName(protocol), func(t *testing.T) {
			response := adapterResponseBody(t, protocol, marker+strings.Repeat("x", MaxResponseBytes))
			server := newAdapterResponseServer(t, response)
			snapshot := validAdapterSnapshot(protocol, server.URL)
			adapter, err := AdapterFor(snapshot, server.Client())
			require.NoError(t, err)

			_, err = adapter.Probe(context.Background(), snapshot, Challenge{Prompt: adapterTestPrompt})

			require.Error(t, err)
			assert.Equal(t, ErrorResponseTooLarge, ErrorCodeOf(err))
			assert.Equal(t, string(ErrorResponseTooLarge), err.Error())
			assert.NotContains(t, err.Error(), marker)
		})
	}
}

func TestOpenAIChatResponsesAnthropicAndGeminiAdaptersRejectNonceMismatchInRunConversation(t *testing.T) {
	for _, protocol := range adapterProtocols() {
		t.Run(protocolName(protocol), func(t *testing.T) {
			server := newAdapterResponseServer(t, adapterResponseBody(t, protocol, "psp_wrong"))
			snapshot := validAdapterSnapshot(protocol, server.URL)
			adapter, err := AdapterFor(snapshot, server.Client())
			require.NoError(t, err)

			result := RunConversation(context.Background(), adapter, snapshot, time.Second, time.Second)

			assert.Equal(t, StateValidationFailed, result.State)
			assert.Equal(t, ErrorValidationFailed, result.Code)
			assert.NotNil(t, result.LatencyMS)
		})
	}
}

func TestGeminiModelIsEscapedIntoPathAndNeverQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/v1beta/models/gemini%2Fpro%3Fcredential=leak:generateContent", request.RequestURI)
		assert.Empty(t, request.URL.RawQuery)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"psp_token"}]}}]}`)
	}))
	t.Cleanup(server.Close)
	snapshot := validAdapterSnapshot(ProtocolGeminiGenerateContent, server.URL)
	snapshot.Model = "gemini/pro?credential=leak"
	adapter, err := AdapterFor(snapshot, server.Client())
	require.NoError(t, err)

	output, err := adapter.Probe(context.Background(), snapshot, Challenge{Prompt: adapterTestPrompt})

	require.NoError(t, err)
	assert.Equal(t, "psp_token", output)
}

func TestProviderAdapterDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirected" {
			redirected.Store(true)
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"should-not-run"}}]}`)
			return
		}
		http.Redirect(w, request, "/redirected", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	snapshot := validAdapterSnapshot(ProtocolOpenAIChat, server.URL)
	adapter, err := AdapterFor(snapshot, server.Client())
	require.NoError(t, err)

	_, err = adapter.Probe(context.Background(), snapshot, Challenge{Prompt: adapterTestPrompt})

	require.Error(t, err)
	assert.Equal(t, ErrorProviderRejected, ErrorCodeOf(err))
	assert.False(t, redirected.Load())
}

func TestProviderAdapterClosesResponseBody(t *testing.T) {
	body := &trackingBody{Reader: bytes.NewBufferString(`{"choices":[{"message":{"content":"psp_token"}}]}`)}
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, Request: request}, nil
	})}
	snapshot := validAdapterSnapshot(ProtocolOpenAIChat, "https://example.com")
	adapter, err := AdapterFor(snapshot, client)
	require.NoError(t, err)

	output, err := adapter.Probe(context.Background(), snapshot, Challenge{Prompt: adapterTestPrompt})

	require.NoError(t, err)
	assert.Equal(t, "psp_token", output)
	assert.True(t, body.closed.Load())
}

func validAdapterSnapshot(protocol Protocol, baseURL string) Snapshot {
	return Snapshot{
		Protocol:     protocol,
		BaseURL:      baseURL,
		APIKey:       "provider-key",
		Model:        "provider-model",
		Organization: "org-id",
	}
}

func adapterProtocols() []Protocol {
	return []Protocol{
		ProtocolOpenAIChat,
		ProtocolOpenAIResponses,
		ProtocolAnthropicMessages,
		ProtocolGeminiGenerateContent,
	}
}

func adapterResponseBody(t *testing.T, protocol Protocol, output string) string {
	t.Helper()
	var response any
	switch protocol {
	case ProtocolOpenAIChat:
		response = map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": output}}}}
	case ProtocolOpenAIResponses:
		response = map[string]any{"output_text": output}
	case ProtocolAnthropicMessages:
		response = map[string]any{"content": []any{map[string]any{"type": "text", "text": output}}}
	case ProtocolGeminiGenerateContent:
		response = map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": output}}}}}}
	default:
		t.Fatalf("unsupported test protocol: %q", protocol)
	}
	body, err := json.Marshal(response)
	require.NoError(t, err)
	return string(body)
}

func newAdapterResponseServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(server.Close)
	return server
}

func protocolName(protocol Protocol) string {
	switch protocol {
	case ProtocolOpenAIChat:
		return "OpenAI Chat"
	case ProtocolOpenAIResponses:
		return "OpenAI Responses"
	case ProtocolAnthropicMessages:
		return "Anthropic"
	case ProtocolGeminiGenerateContent:
		return "Gemini"
	default:
		return "Unknown"
	}
}
