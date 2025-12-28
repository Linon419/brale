package provider

import (
	"fmt"
	"strings"
	"time"

	"brale/internal/logger"
)

type ModelCfg struct {
	ID, Provider, APIURL, APIKey, Model string
	Enabled                             bool
	Headers                             map[string]string
	SupportsVision                      bool
	ExpectJSON                          bool
}

func BuildProvidersFromConfig(models []ModelCfg, timeout time.Duration) []ModelProvider {
	out := make([]ModelProvider, 0, len(models))
	for _, m := range models {
		if !m.Enabled {
			continue
		}
		id := strings.TrimSpace(m.ID)
		if id == "" {
			base := strings.TrimSpace(m.Provider)
			if base == "" {
				base = "provider"
			}
			model := strings.TrimSpace(m.Model)
			if model != "" {
				id = fmt.Sprintf("%s:%s", base, model)
			} else {
				id = base
			}
			logger.Warnf("未配置 ai.models.id，已为 %q 生成 ID: %s", m.Provider, id)
		}
		providerName := strings.ToLower(strings.TrimSpace(m.Provider))
		useAnthropic := providerName == "anthropic"
		if !useAnthropic && providerName == "claude" && strings.Contains(strings.ToLower(m.APIURL), "anthropic") {
			useAnthropic = true
		}
		if useAnthropic {
			client := &AnthropicClient{
				BaseURL:      m.APIURL,
				APIKey:       m.APIKey,
				Model:        m.Model,
				ExtraHeaders: m.Headers,
			}
			if timeout > 0 {
				client.Timeout = timeout
			}
			out = append(out, NewAnthropicModelProvider(id, true, m.SupportsVision, m.ExpectJSON, client))
			continue
		}
		if providerName == "gemini" || providerName == "google" || providerName == "genai" {
			client := &GeminiClient{
				BaseURL:      m.APIURL,
				APIKey:       m.APIKey,
				Model:        m.Model,
				ExtraHeaders: m.Headers,
			}
			if timeout > 0 {
				client.Timeout = timeout
			}
			out = append(out, NewGeminiModelProvider(id, true, m.SupportsVision, m.ExpectJSON, client))
			continue
		}
		client := &OpenAIChatClient{
			BaseURL:      m.APIURL,
			APIKey:       m.APIKey,
			Model:        m.Model,
			ExtraHeaders: m.Headers,
		}
		if timeout > 0 {
			client.Timeout = timeout
		}
		out = append(out, NewOpenAIModelProvider(id, true, m.SupportsVision, m.ExpectJSON, client))
	}
	return out
}
