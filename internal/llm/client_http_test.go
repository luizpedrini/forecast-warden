package llm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luizpedrini/forecast-warden/internal/llm"
)

func TestOpenAIClientSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth=%s", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["model"] != "gpt-4o-mini" {
			t.Errorf("model=%v", req["model"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"synthesis":"ok","hypotheses":[]}`}},
			},
		})
	}))
	defer srv.Close()

	c := llm.NewOpenAIClient("test-key", "gpt-4o-mini")
	c.BaseURL = srv.URL
	c.HTTP = srv.Client()
	out, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "synthesis") {
		t.Fatalf("out=%s", out)
	}
}

func TestAnthropicClientSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "ant-key" {
			t.Errorf("key=%s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": `{"synthesis":"hi","hypotheses":[{"name":"calendar","why":"season"}]}`},
			},
		})
	}))
	defer srv.Close()

	c := llm.NewAnthropicClient("ant-key", "claude-3-5-haiku-latest")
	c.BaseURL = srv.URL
	c.HTTP = srv.Client()
	out, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "calendar") {
		t.Fatalf("out=%s", out)
	}
}

func TestOpenAIClientHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()
	c := llm.NewOpenAIClient("bad", "gpt-4o-mini")
	c.BaseURL = srv.URL
	c.HTTP = srv.Client()
	_, err := c.Complete(context.Background(), "s", "u")
	if err == nil {
		t.Fatal("expected error")
	}
}
