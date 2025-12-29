package middlewares

import (
	"context"
	"fmt"
	"strings"
	"time"

	"brale/internal/market"
	"brale/internal/pipeline"
	"brale/internal/strategy"

	talib "github.com/markcheno/go-talib"
)

type EMATrendConfig struct {
	Name     string
	Stage    int
	Critical bool
	Timeout  time.Duration
	Interval string
	Fast     int
	Mid      int
	Slow     int
	Long     int
}

type EMATrendMiddleware struct {
	meta     pipeline.MiddlewareMeta
	interval string
	fast     int
	mid      int
	slow     int
	long     int
}

func NewEMATrend(cfg EMATrendConfig) *EMATrendMiddleware {
	return &EMATrendMiddleware{
		meta: pipeline.MiddlewareMeta{
			Name:     cfg.Name,
			Stage:    cfg.Stage,
			Critical: cfg.Critical,
			Timeout:  cfg.Timeout,
		},
		interval: cfg.Interval,
		fast:     cfg.Fast,
		mid:      cfg.Mid,
		slow:     cfg.Slow,
		long:     cfg.Long,
	}
}

func (m *EMATrendMiddleware) GetConfig() EMATrendConfig {
	return EMATrendConfig{
		Name:     m.meta.Name,
		Stage:    m.meta.Stage,
		Critical: m.meta.Critical,
		Timeout:  m.meta.Timeout,
		Interval: m.interval,
		Fast:     m.fast,
		Mid:      m.mid,
		Slow:     m.slow,
		Long:     m.long,
	}
}

func (m *EMATrendMiddleware) Meta() pipeline.MiddlewareMeta { return m.meta }

func (m *EMATrendMiddleware) Handle(ctx context.Context, ac *pipeline.AnalysisContext) error {
	interval := m.interval
	if interval == "" {
		interval = "1h"
	}
	candles := ac.Candles(interval)
	if len(candles) == 0 {
		return fmt.Errorf("ema_trend: no candles for %s", interval)
	}
	closes := closes(candles)
	fast := strategy.EMA(closes, m.fast)
	mid := strategy.EMA(closes, m.mid)
	slow := strategy.EMA(closes, m.slow)
	long := strategy.EMA(closes, m.long)
	trend := strategy.ClassifyTrend4(fast, mid, slow, long)
	trendLabel := map[string]string{
		"UP":    "上升",
		"DOWN":  "下行",
		"MIXED": "震荡",
	}[trend]
	if trendLabel == "" {
		trendLabel = "未知"
	}
	spreadFastMid := fast - mid
	spreadMidSlow := mid - slow
	spreadSlowLong := slow - long
	desc := fmt.Sprintf("周期 %s 的 EMA(%d/%d/%d/%d) 原始数值：fast=%.4f、mid=%.4f、slow=%.4f、long=%.4f",
		strings.ToUpper(interval), m.fast, m.mid, m.slow, m.long, fast, mid, slow, long)
	ac.AddFeature(pipeline.Feature{
		Key:         "ema_trend",
		Label:       fmt.Sprintf("%s EMA", strings.ToUpper(interval)),
		Value:       fast - long,
		Description: formatFeature(ac.Symbol, desc),
		Metadata: map[string]any{
			"interval":        interval,
			"ema_fast":        fast,
			"ema_mid":         mid,
			"ema_slow":        slow,
			"ema_long":        long,
			"spread_fast_mid": spreadFastMid,
			"spread_mid_slow": spreadMidSlow,
			"spread_slow_long": spreadSlowLong,
			"trend":           trend,
			"trend_label":     trendLabel,
			"pivots":          emaPivots(candles, m.fast, m.mid, m.slow, m.long),
		},
	})
	return nil
}

type emaPivot struct {
	Type string  `json:"type"`
	Time string  `json:"time"`
	Val  float64 `json:"value"`
}

func emaPivots(candles []market.Candle, fastPeriod, midPeriod, slowPeriod, longPeriod int) []emaPivot {
	if len(candles) == 0 {
		return nil
	}
	if !sufficientEmaHistory(candles, fastPeriod, midPeriod, slowPeriod, longPeriod) {
		return nil
	}
	closes := closes(candles)
	fastArr := talib.Ema(closes, fastPeriod)
	midArr := talib.Ema(closes, midPeriod)
	slowArr := talib.Ema(closes, slowPeriod)
	longArr := talib.Ema(closes, longPeriod)
	pivots := make([]emaPivot, 0, 4)

	// We cap pivot count to avoid flooding the prompt (fast-mid up to 8; mid-slow up to 12; slow-long up to 16).
	appendFastMidPivots(candles, fastArr, midArr, &pivots)
	appendMidSlowPivots(candles, midArr, slowArr, &pivots)
	appendSlowLongPivots(candles, slowArr, longArr, &pivots)
	return pivots
}

func sufficientEmaHistory(candles []market.Candle, fastPeriod, midPeriod, slowPeriod, longPeriod int) bool {
	maxPeriod := max4(fastPeriod, midPeriod, slowPeriod, longPeriod)
	return maxPeriod > 0 && len(candles) >= maxPeriod
}

func max4(a, b, c, d int) int {
	if a < b {
		a = b
	}
	if a < c {
		a = c
	}
	if a < d {
		return d
	}
	return a
}

func appendFastMidPivots(candles []market.Candle, fastArr, midArr []float64, pivots *[]emaPivot) {
	if len(fastArr) < len(candles) || pivots == nil {
		return
	}
	for i := len(fastArr) - 1; i > 1 && len(*pivots) < 8; i-- {
		cur := fastArr[i] - midArr[i]
		prev := fastArr[i-1] - midArr[i-1]
		if (cur >= 0 && prev < 0) || (cur <= 0 && prev > 0) {
			appendPivot(candles, i, "fast-mid crossover", fastArr[i], pivots)
		}
	}
}

func appendMidSlowPivots(candles []market.Candle, midArr, slowArr []float64, pivots *[]emaPivot) {
	if len(midArr) < len(candles) || pivots == nil {
		return
	}
	for i := len(midArr) - 1; i > 1 && len(*pivots) < 12; i-- {
		cur := midArr[i] - slowArr[i]
		prev := midArr[i-1] - slowArr[i-1]
		if (cur >= 0 && prev < 0) || (cur <= 0 && prev > 0) {
			appendPivot(candles, i, "mid-slow crossover", midArr[i], pivots)
		}
	}
}

func appendSlowLongPivots(candles []market.Candle, slowArr, longArr []float64, pivots *[]emaPivot) {
	if len(longArr) < len(candles) || pivots == nil {
		return
	}
	for i := len(longArr) - 1; i > 1 && len(*pivots) < 16; i-- {
		cur := slowArr[i] - longArr[i]
		prev := slowArr[i-1] - longArr[i-1]
		if (cur >= 0 && prev < 0) || (cur <= 0 && prev > 0) {
			appendPivot(candles, i, "slow-long crossover", slowArr[i], pivots)
		}
	}
}

// appendPivot converts candle index into timestamped pivot entry.
// Example: if fast crosses mid at idx=10, we emit {"type": "fast-mid crossover", "time": <candle[10] time>, "value": fast[10]}.
func appendPivot(candles []market.Candle, idx int, label string, val float64, pivots *[]emaPivot) {
	if pivots == nil || idx < 0 || idx >= len(candles) {
		return
	}
	ts := candles[idx].CloseTime
	if ts == 0 {
		ts = candles[idx].OpenTime
	}
	*pivots = append(*pivots, emaPivot{
		Type: label,
		Time: time.UnixMilli(ts).UTC().Format(time.RFC3339),
		Val:  val,
	})
}
