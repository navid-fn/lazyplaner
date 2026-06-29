package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

func NewClient(baseURL string, model string) *Client {
	trimmedBaseURL := strings.TrimRight(baseURL, "/")
	trimmedModel := strings.TrimSpace(model)
	if trimmedBaseURL == "" {
		trimmedBaseURL = "http://localhost:11434"
	}
	if trimmedModel == "" {
		trimmedModel = "llama3.2"
	}

	return &Client{
		baseURL: trimmedBaseURL,
		model:   trimmedModel,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *Client) GenerateDescription(ctx context.Context, subject string, detail string) (string, error) {
	trimmedSubject := strings.TrimSpace(subject)
	trimmedDetail := strings.TrimSpace(detail)
	if trimmedSubject == "" || trimmedDetail == "" {
		return "", errors.New("subject and detail are required")
	}

	prompt := fmt.Sprintf(
		"Subject: %s\nDetail: %s\n\nReturn a concise markdown description only.",
		trimmedSubject,
		trimmedDetail,
	)

	payload := struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		System string `json:"system"`
		Stream bool   `json:"stream"`
	}{
		Model:  c.model,
		Prompt: prompt,
		System: "You are a helpful planner. Respond with markdown only.",
		Stream: false,
	}

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

	trimmedResponse := strings.TrimSpace(result.Response)
	if trimmedResponse == "" {
		return "", errors.New("ollama returned empty response")
	}

	return trimmedResponse, nil
}
