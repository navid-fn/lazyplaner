package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// GenerateJSON asks the model for a reply that matches schema. Ollama enforces
// the schema while decoding, so the returned text is JSON with the required
// fields. The caller still validates values such as ranges and enums.
func (c *Client) GenerateJSON(ctx context.Context, system string, prompt string, schema json.RawMessage) (string, error) {
	payload := struct {
		Model   string          `json:"model"`
		Prompt  string          `json:"prompt"`
		System  string          `json:"system"`
		Stream  bool            `json:"stream"`
		Format  json.RawMessage `json:"format"`
		Options struct {
			Temperature float64 `json:"temperature"`
		} `json:"options"`
	}{
		Model:  c.Model(),
		Prompt: prompt,
		System: system,
		Stream: false,
		Format: schema,
	}
	// Temperature 0 keeps the same plan mapping to the same skill each time.
	payload.Options.Temperature = 0

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("serialize request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("ollama error: %s", response.Status)
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	trimmed := strings.TrimSpace(result.Response)
	if trimmed == "" {
		return "", errors.New("ollama returned empty response")
	}
	return trimmed, nil
}
