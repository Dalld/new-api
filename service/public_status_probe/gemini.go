package public_status_probe

import (
	"context"
	"net/http"
	"net/url"
)

type geminiAdapter struct {
	client *http.Client
}

func (a geminiAdapter) Probe(ctx context.Context, snapshot Snapshot, challenge Challenge) (string, error) {
	base, err := validateSnapshot(snapshot)
	if err != nil {
		return "", err
	}
	target := geminiEndpointURL(base, snapshot.Model)
	request := geminiRequest{
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: challenge.Prompt}}},
		},
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens: probeOutputTokens,
			Temperature:     0,
		},
	}
	headers := make(http.Header, 1)
	headers.Set("x-goog-api-key", snapshot.APIKey)

	var response geminiResponse
	if err := sendJSONProbe(ctx, a.client, target, headers, request, &response); err != nil {
		return "", err
	}
	if len(response.Candidates) == 0 {
		return "", codedError(ErrorValidationFailed)
	}

	parts := make([]string, 0, len(response.Candidates[0].Content.Parts))
	found := false
	for _, part := range response.Candidates[0].Content.Parts {
		if part.Text == nil {
			continue
		}
		found = true
		parts = append(parts, *part.Text)
	}
	if !found {
		return "", codedError(ErrorValidationFailed)
	}
	return boundedText(parts...)
}

func geminiEndpointURL(base *url.URL, model string) string {
	target := *base
	target.Path = joinEndpointPath(base.Path, "/v1beta/models/"+model+":generateContent")
	target.RawPath = joinEndpointPath(base.EscapedPath(), "/v1beta/models/"+url.PathEscape(model)+":generateContent")
	if target.RawPath == target.Path {
		target.RawPath = ""
	}
	return target.String()
}

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens"`
	Temperature     float64 `json:"temperature"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text *string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}
