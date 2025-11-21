package decision

import "time"

// 中文说明：
// 本文件定义 AI 决策相关的通用数据结构，供引擎与聚合器使用。

// Decision 单笔 AI 决策动作（与旧版保持兼容的最小字段集）
type Decision struct {
	Symbol          string         `json:"symbol"`
	Action          string         `json:"action"`             // open_long/open_short/close_long/close_short/hold/wait
	Leverage        int            `json:"leverage,omitempty"` // 杠杆（仅开仓）
	PositionSizeUSD float64        `json:"position_size_usd,omitempty"`
	CloseRatio      float64        `json:"close_ratio,omitempty"`
	StopLoss        float64        `json:"stop_loss,omitempty"`
	TakeProfit      float64        `json:"take_profit,omitempty"`
	Confidence      int            `json:"confidence,omitempty"`
	Reasoning       string         `json:"reasoning,omitempty"`
	Tiers           *DecisionTiers `json:"tiers,omitempty"`
}

// DecisionTiers 描述三段式离场的目标与比例。
type DecisionTiers struct {
	Tier1Target float64 `json:"tier1_target,omitempty"`
	Tier1Ratio  float64 `json:"tier1_ratio,omitempty"`
	Tier2Target float64 `json:"tier2_target,omitempty"`
	Tier2Ratio  float64 `json:"tier2_ratio,omitempty"`
	Tier3Target float64 `json:"tier3_target,omitempty"`
	Tier3Ratio  float64 `json:"tier3_ratio,omitempty"`
}

// DecisionResult AI 决策输出（可包含多币种）
type DecisionResult struct {
	Decisions []Decision
	RawOutput string // 原始模型完整输出（便于调试/提取思维链）
	RawJSON   string // 提取到的 JSON 决策数组文本
	// MetaSummary: 当使用 meta 聚合时，记录各模型投票与理由的汇总文本（用于通知）
	MetaSummary string
	TraceID     string
}

// DecisionMemory 记录上一轮决策（供 Prompt 回顾）。
type DecisionMemory struct {
	Symbol    string     `json:"symbol"`
	Horizon   string     `json:"horizon"`
	DecidedAt time.Time  `json:"decided_at"`
	Decisions []Decision `json:"decisions"`
}

// LastDecisionRecord 用于持久化最近一次决策，供重启后恢复上下文。
type LastDecisionRecord struct {
	Symbol    string
	Horizon   string
	DecidedAt time.Time
	Decisions []Decision
}
