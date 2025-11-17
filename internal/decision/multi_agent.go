package decision

import (
	"fmt"
	"strings"

	brcfg "brale/internal/config"
)

// AgentInsight 记录多阶段 Agent 的文本输出。
type AgentInsight struct {
	Stage      string `json:"stage"`
	ProviderID string `json:"provider_id"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	Warned     bool   `json:"warned,omitempty"`
	System     string `json:"system,omitempty"`
	User       string `json:"user,omitempty"`
}

const (
	agentStageIndicator = "indicator"
	agentStagePattern   = "pattern"
	agentStageTrend     = "trend"
)

func agentBlockLimit(cfg brcfg.MultiAgentConfig) int {
	if cfg.MaxBlocks <= 0 {
		return 4
	}
	if cfg.MaxBlocks > 8 {
		return 8
	}
	return cfg.MaxBlocks
}

func buildIndicatorAgentPrompt(ctxs []AnalysisContext, cfg brcfg.MultiAgentConfig) string {
	if len(ctxs) == 0 {
		return ""
	}
	limit := agentBlockLimit(cfg)
	var b strings.Builder
	b.WriteString("# Technical Indicator Blocks\n")
	count := 0
	for _, ac := range ctxs {
		data := strings.TrimSpace(ac.IndicatorJSON)
		if data == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("## %s %s (%s)\n", ac.Symbol, ac.Interval, ac.ForecastHorizon))
		b.WriteString(data)
		b.WriteString("\n\n")
		count++
		if count >= limit {
			break
		}
	}
	if count == 0 {
		return ""
	}
	b.WriteString("请总结动能、量价与波动率，并点名最强与最弱周期。\n")
	return b.String()
}

func buildPatternAgentPrompt(ctxs []AnalysisContext, cfg brcfg.MultiAgentConfig) string {
	if len(ctxs) == 0 {
		return ""
	}
	limit := agentBlockLimit(cfg)
	var b strings.Builder
	b.WriteString("# Pattern & Narrative Blocks\n")
	count := 0
	for _, ac := range ctxs {
		pattern := strings.TrimSpace(ac.PatternReport)
		trend := strings.TrimSpace(ac.TrendReport)
		note := strings.TrimSpace(ac.ImageNote)
		if pattern == "" && trend == "" && note == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("## %s %s (%s)\n", ac.Symbol, ac.Interval, ac.ForecastHorizon))
		if pattern != "" {
			b.WriteString("- Pattern: " + pattern + "\n")
		}
		if trend != "" {
			b.WriteString("- Trend: " + trend + "\n")
		}
		if note != "" {
			b.WriteString("- Visual: " + note + "\n")
		}
		b.WriteString("\n")
		count++
		if count >= limit {
			break
		}
	}
	if count == 0 {
		return ""
	}
	b.WriteString("识别多空冲突、图形触发点与SMC叙事，并按优先级输出。\n")
	return b.String()
}

func buildTrendAgentPrompt(ctxs []AnalysisContext, cfg brcfg.MultiAgentConfig) string {
	if len(ctxs) == 0 {
		return ""
	}
	limit := agentBlockLimit(cfg)
	var b strings.Builder
	b.WriteString("# Raw Kline Windows\n")
	count := 0
	for _, ac := range ctxs {
		data := strings.TrimSpace(ac.KlineJSON)
		note := strings.TrimSpace(ac.ImageNote)
		if data == "" && note == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("## %s %s (%s)\n", ac.Symbol, ac.Interval, ac.ForecastHorizon))
		if data != "" {
			b.WriteString("Raw: \n")
			b.WriteString(data)
			b.WriteString("\n")
		}
		if note != "" {
			b.WriteString("Visual: " + note + "\n")
		}
		if strings.TrimSpace(ac.TrendReport) != "" {
			b.WriteString("Trend: " + strings.TrimSpace(ac.TrendReport) + "\n")
		}
		b.WriteString("\n")
		count++
		if count >= limit {
			break
		}
	}
	if count == 0 {
		return ""
	}
	b.WriteString("请找出关键支撑/阻力、动量加速或背离。\n")
	return b.String()
}

func formatAgentStageTitle(stage string) string {
	switch stage {
	case agentStageIndicator:
		return "Indicator Agent"
	case agentStagePattern:
		return "Pattern Agent"
	case agentStageTrend:
		return "Trend Agent"
	default:
		stage = strings.TrimSpace(stage)
		if stage == "" {
			return "Agent"
		}
		if len(stage) == 1 {
			return strings.ToUpper(stage) + " Agent"
		}
		return strings.ToUpper(stage[:1]) + stage[1:] + " Agent"
	}
}
