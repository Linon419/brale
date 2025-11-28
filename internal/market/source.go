package market

import "context"

// CandleEvent 封装了来源于外部行情源的单根 K 线。
type CandleEvent struct {
	Symbol   string
	Interval string
	Candle   Candle
}

// TradeEvent 表示实时成交价事件（例如 aggTrade）。
type TradeEvent struct {
	Symbol    string
	Price     float64
	Quantity  float64
	EventTime int64
	TradeTime int64
}

// SubscribeOptions 控制实时订阅行为。
type SubscribeOptions struct {
	BatchSize    int
	Buffer       int
	OnConnect    func()
	OnDisconnect func(error)
}

// SourceStats 记录数据源运行期的一些指标。
type SourceStats struct {
	Reconnects      int
	SubscribeErrors int
	LastError       string
}

// Source 统一对接外部行情供应商。
type Source interface {
	// FetchHistory 拉取最近 limit 根 K 线并按时间升序返回。
	FetchHistory(ctx context.Context, symbol, interval string, limit int) ([]Candle, error)
	// Subscribe 订阅实时 K 线，返回只读事件通道；通道关闭意味着订阅已结束。
	Subscribe(ctx context.Context, symbols, intervals []string, opts SubscribeOptions) (<-chan CandleEvent, error)
	// SubscribeTrades 订阅实时成交价（如 aggTrade），供策略使用真实成交价触发。
	SubscribeTrades(ctx context.Context, symbols []string, opts SubscribeOptions) (<-chan TradeEvent, error)
	// Stats 返回当前运行状态（若 source 不支持则返回零值）。
	Stats() SourceStats
	// Close 释放底层资源，例如关闭 WS 连接。
	Close() error
}
