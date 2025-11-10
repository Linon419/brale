package provider

import (
	"fmt"
	"strings"
	"time"

	"brale/internal/logger"
)

// 配置驱动的 Provider 工厂（不再使用环境变量）。

// 中文说明：
// 根据配置构造模型提供方列表；若未显式提供 id，则自动生成稳定 ID，避免日志为空。

// BuildMCPProviders 简化工厂：按 flags 启用 deepseek/qwen 两类 provider
type ModelCfg struct {
	ID, Provider, APIURL, APIKey, Model string
	Enabled                             bool
	Headers                             map[string]string // 额外请求头（如 X-API-Key / Organization）
}

// BuildProvidersFromConfig 根据配置文件的模型条目构造 Provider 列表
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
		client := &OpenAIChatClient{
			BaseURL:      m.APIURL,
			APIKey:       m.APIKey,
			Model:        m.Model,
			ExtraHeaders: m.Headers,
		}
		if timeout > 0 {
			client.Timeout = timeout
		}
		out = append(out, NewOpenAIModelProvider(id, true, client))
	}
	return out
}
