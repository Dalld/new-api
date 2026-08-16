package public_status_probe

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	maxBaseURLBytes      = 4096
	maxAPIKeyBytes       = 8192
	maxModelBytes        = 512
	maxOrganizationBytes = 512
	probeOutputTokens    = 24
)

type openAIChatAdapter struct {
	client *http.Client
}

type openAIResponsesAdapter struct {
	client *http.Client
}

// Snapshot intentionally supports only static API-key authentication. Per-target
// custom headers, proxies, OAuth, and Codex-specific credentials are not accepted.
func AdapterFor(snapshot Snapshot, client *http.Client) (Adapter, error) {
	var adapter Adapter
	switch snapshot.Protocol {
	case ProtocolOpenAIChat:
		adapter = openAIChatAdapter{client: client}
	case ProtocolOpenAIResponses:
		adapter = openAIResponsesAdapter{client: client}
	case ProtocolAnthropicMessages:
		adapter = anthropicMessagesAdapter{client: client}
	case ProtocolGeminiGenerateContent:
		adapter = geminiAdapter{client: client}
	default:
		return nil, codedError(ErrorUnsupportedProvider)
	}
	if _, err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	return adapter, nil
}

func (a openAIChatAdapter) Probe(ctx context.Context, snapshot Snapshot, challenge Challenge) (string, error) {
	base, err := validateSnapshot(snapshot)
	if err != nil {
		return "", err
	}
	target := endpointURL(base, "/v1/chat/completions")
	request := openAIChatRequest{
		Model: snapshot.Model,
		Messages: []openAIChatMessage{
			{Role: "user", Content: challenge.Prompt},
		},
		MaxTokens:   probeOutputTokens,
		Stream:      false,
		Temperature: 0,
	}
	var response openAIChatResponse
	if err := sendJSONProbe(ctx, a.client, target, openAIHeaders(snapshot), request, &response); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 || response.Choices[0].Message.Content == nil {
		return "", codedError(ErrorValidationFailed)
	}
	return boundedText(*response.Choices[0].Message.Content)
}

func (a openAIResponsesAdapter) Probe(ctx context.Context, snapshot Snapshot, challenge Challenge) (string, error) {
	base, err := validateSnapshot(snapshot)
	if err != nil {
		return "", err
	}
	target := endpointURL(base, "/v1/responses")
	request := openAIResponsesRequest{
		Model:           snapshot.Model,
		Input:           challenge.Prompt,
		MaxOutputTokens: probeOutputTokens,
		Stream:          false,
	}
	var response openAIResponsesResponse
	if err := sendJSONProbe(ctx, a.client, target, openAIHeaders(snapshot), request, &response); err != nil {
		return "", err
	}
	if response.OutputText != nil {
		return boundedText(*response.OutputText)
	}

	parts := make([]string, 0)
	found := false
	for _, output := range response.Output {
		for _, content := range output.Content {
			if content.Type != "output_text" || content.Text == nil {
				continue
			}
			found = true
			parts = append(parts, *content.Text)
		}
	}
	if !found {
		return "", codedError(ErrorValidationFailed)
	}
	return boundedText(parts...)
}

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	MaxTokens   int                 `json:"max_tokens"`
	Stream      bool                `json:"stream"`
	Temperature float64             `json:"temperature"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type openAIResponsesRequest struct {
	Model           string `json:"model"`
	Input           string `json:"input"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	Stream          bool   `json:"stream"`
}

type openAIResponsesResponse struct {
	OutputText *string `json:"output_text"`
	Output     []struct {
		Content []struct {
			Type string  `json:"type"`
			Text *string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func validateSnapshot(snapshot Snapshot) (*url.URL, error) {
	if len(snapshot.BaseURL) == 0 || len(snapshot.BaseURL) > maxBaseURLBytes || !utf8.ValidString(snapshot.BaseURL) {
		return nil, codedError(ErrorInvalidTarget)
	}
	parsed, err := url.Parse(snapshot.BaseURL)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, codedError(ErrorInvalidTarget)
	}
	if parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return nil, codedError(ErrorInvalidTarget)
	}
	if port := parsed.Port(); port != "" {
		portNumber, portErr := strconv.ParseUint(port, 10, 16)
		if portErr != nil || portNumber == 0 {
			return nil, codedError(ErrorInvalidTarget)
		}
	}
	if !safeBasePath(parsed) {
		return nil, codedError(ErrorInvalidTarget)
	}
	if len(snapshot.APIKey) == 0 || len(snapshot.APIKey) > maxAPIKeyBytes || !utf8.ValidString(snapshot.APIKey) || strings.TrimSpace(snapshot.APIKey) != snapshot.APIKey || containsInvalidHeaderByte(snapshot.APIKey) {
		return nil, codedError(ErrorInvalidTarget)
	}
	if len(snapshot.Model) == 0 || len(snapshot.Model) > maxModelBytes || !utf8.ValidString(snapshot.Model) || strings.TrimSpace(snapshot.Model) != snapshot.Model {
		return nil, codedError(ErrorInvalidTarget)
	}
	if len(snapshot.Organization) > maxOrganizationBytes || !utf8.ValidString(snapshot.Organization) || strings.TrimSpace(snapshot.Organization) != snapshot.Organization || containsInvalidHeaderByte(snapshot.Organization) {
		return nil, codedError(ErrorInvalidTarget)
	}
	return parsed, nil
}

func safeBasePath(parsed *url.URL) bool {
	if parsed.RawPath != "" || strings.Contains(parsed.Path, "\\") {
		return false
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return true
	}
	trimmed := strings.TrimSuffix(parsed.Path, "/")
	return strings.HasPrefix(trimmed, "/") && path.Clean(trimmed) == trimmed
}

func containsInvalidHeaderByte(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] == 0x7f {
			return true
		}
	}
	return false
}

func endpointURL(base *url.URL, endpoint string) string {
	joinedPath := joinEndpointPath(base.Path, endpoint)
	target := *base
	target.Path = joinedPath
	target.RawPath = ""
	return target.String()
}

func joinEndpointPath(basePath, endpoint string) string {
	basePath = strings.TrimSuffix(basePath, "/")
	segments := strings.Split(strings.TrimPrefix(endpoint, "/"), "/")
	if len(segments) > 0 && strings.HasSuffix(basePath, "/"+segments[0]) {
		endpoint = strings.TrimPrefix(endpoint, "/"+segments[0])
	}
	return basePath + endpoint
}

func openAIHeaders(snapshot Snapshot) http.Header {
	headers := make(http.Header, 3)
	headers.Set("Authorization", "Bearer "+snapshot.APIKey)
	if snapshot.Organization != "" {
		headers.Set("OpenAI-Organization", snapshot.Organization)
	}
	return headers
}

func sendJSONProbe(ctx context.Context, client *http.Client, target string, headers http.Header, requestBody any, responseBody any) error {
	body, err := common.Marshal(requestBody)
	if err != nil {
		return codedError(ErrorValidationFailed)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return codedError(ErrorInvalidTarget)
	}
	request.Header = headers.Clone()
	request.Header.Set("Content-Type", "application/json")

	response, err := DoBounded(client, request)
	if err != nil {
		return err
	}
	if err := common.Unmarshal(response, responseBody); err != nil {
		return codedError(ErrorValidationFailed)
	}
	return nil
}

func boundedText(parts ...string) (string, error) {
	var output strings.Builder
	for _, part := range parts {
		if len(part) > MaxResponseBytes-output.Len() {
			return "", codedError(ErrorResponseTooLarge)
		}
		output.WriteString(part)
	}
	value := output.String()
	if strings.TrimSpace(value) == "" {
		return "", codedError(ErrorEmptyResponse)
	}
	return value, nil
}
