package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Gemini exposes an OpenAI-compatible chat completions endpoint.
// See: https://ai.google.dev/gemini-api/docs/openai
const geminiChatURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"

// GeminiClient calls the Gemini API via its OpenAI-compatible REST endpoint
// using stdlib net/http (no external SDK).
type GeminiClient struct {
	APIKey  string
	Model   string
	BaseURL string
	HTTP    *http.Client
}

func NewGeminiClient(apiKey, model string) *GeminiClient {
	return &GeminiClient{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: geminiChatURL,
		HTTP:    &http.Client{Timeout: defaultTimeout},
	}
}

// geminiRequest matches the OpenAI chat completions shape that Gemini accepts.
type geminiRequest struct {
	Model       string          `json:"model"`
	Messages    []geminiMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
}

type geminiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type geminiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func (c *GeminiClient) Complete(ctx context.Context, system, user string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("gemini client is nil")
	}
	body := geminiRequest{
		Model: c.Model,
		Messages: []geminiMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	url := c.BaseURL
	if url == "" {
		url = geminiChatURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var parsed geminiResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("gemini decode: %w (status %d)", err, resp.StatusCode)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("gemini: %s", parsed.Error.Message)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gemini HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("gemini: empty completion")
	}
	return parsed.Choices[0].Message.Content, nil
}
