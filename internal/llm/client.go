// Package llm provides optional LLM enrichment for investigation playbooks.
// API keys come from the environment only; CI uses mock clients (no live API).
package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// Client is a pluggable chat-completion interface.
type Client interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Provider names accepted by --provider / WARDEN_LLM_PROVIDER.
const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
)

const (
	envProvider     = "WARDEN_LLM_PROVIDER"
	envModel        = "WARDEN_LLM_MODEL"
	envOpenAIKey    = "OPENAI_API_KEY"
	envAnthropicKey = "ANTHROPIC_API_KEY"

	defaultOpenAIModel    = "gpt-4o-mini"
	defaultAnthropicModel = "claude-3-5-haiku-latest"
	defaultTimeout        = 45 * time.Second
)

// ResolveProvider returns the provider from flag, else WARDEN_LLM_PROVIDER, else openai.
func ResolveProvider(flag string) string {
	p := strings.TrimSpace(strings.ToLower(flag))
	if p == "" {
		p = strings.TrimSpace(strings.ToLower(os.Getenv(envProvider)))
	}
	if p == "" {
		return ProviderOpenAI
	}
	return p
}

// ResolveModel returns WARDEN_LLM_MODEL or a provider-sensible default.
func ResolveModel(provider string) string {
	if m := strings.TrimSpace(os.Getenv(envModel)); m != "" {
		return m
	}
	switch ResolveProvider(provider) {
	case ProviderAnthropic:
		return defaultAnthropicModel
	default:
		return defaultOpenAIModel
	}
}

// APIKeyFor returns the env API key for the provider.
func APIKeyFor(provider string) (string, error) {
	switch ResolveProvider(provider) {
	case ProviderOpenAI:
		k := strings.TrimSpace(os.Getenv(envOpenAIKey))
		if k == "" {
			return "", fmt.Errorf("missing %s (required for --llm --provider openai)", envOpenAIKey)
		}
		return k, nil
	case ProviderAnthropic:
		k := strings.TrimSpace(os.Getenv(envAnthropicKey))
		if k == "" {
			return "", fmt.Errorf("missing %s (required for --llm --provider anthropic)", envAnthropicKey)
		}
		return k, nil
	default:
		return "", fmt.Errorf("unknown LLM provider %q (want openai|anthropic)", provider)
	}
}

// NewClientFromEnv builds an HTTP client for the given provider using env keys/models.
func NewClientFromEnv(provider string) (Client, error) {
	p := ResolveProvider(provider)
	key, err := APIKeyFor(p)
	if err != nil {
		return nil, err
	}
	model := ResolveModel(p)
	switch p {
	case ProviderOpenAI:
		return NewOpenAIClient(key, model), nil
	case ProviderAnthropic:
		return NewAnthropicClient(key, model), nil
	default:
		return nil, fmt.Errorf("unknown LLM provider %q (want openai|anthropic)", p)
	}
}
