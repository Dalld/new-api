package public_status_probe

import (
	"context"
	"net/http"
)

type anthropicMessagesAdapter struct {
	client *http.Client
}

func (a anthropicMessagesAdapter) Probe(ctx context.Context, snapshot Snapshot, challenge Challenge) (string, error) {
	base, err := validateSnapshot(snapshot)
	if err != nil {
		return "", err
	}
	target := endpointURL(base, "/v1/messages")
	request := anthropicRequest{
		Model:     snapshot.Model,
		MaxTokens: probeOutputTokens,
		Messages: []anthropicMessage{
			{Role: "user", Content: challenge.Prompt},
		},
	}
	headers := make(http.Header, 2)
	headers.Set("x-api-key", snapshot.APIKey)
	headers.Set("anthropic-version", "2023-06-01")

	var response anthropicResponse
	if err := sendJSONProbe(ctx, a.client, target, headers, request, &response); err != nil {
		return "", err
	}
	if response.Type == "error" || response.Error != nil {
		return "", codedError(ErrorProviderRejected)
	}

	parts := make([]string, 0, len(response.Content))
	found := false
	for _, content := range response.Content {
		if content.Type != "text" || content.Text == nil {
			continue
		}
		found = true
		parts = append(parts, *content.Text)
	}
	if !found {
		return "", codedError(ErrorEmptyResponse)
	}
	return boundedText(parts...)
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Type  string `json:"type"`
	Error *struct {
		Type string `json:"type"`
	} `json:"error"`
	Content []struct {
		Type string  `json:"type"`
		Text *string `json:"text"`
	} `json:"content"`
}
