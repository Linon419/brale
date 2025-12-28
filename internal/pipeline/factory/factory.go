package factory

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"brale/internal/config/loader"
	"brale/internal/logger"
	"brale/internal/pipeline"
	"brale/internal/pipeline/middlewares"
	"brale/internal/store"
)

type Factory struct {
	Exporter         store.SnapshotExporter
	DefaultIntervals []string
	DefaultLimit     int
}

func (f *Factory) Build(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	name := strings.TrimSpace(cfg.Name)
	switch name {
	case "", "kline_fetcher":
		return f.buildCandleFetcher(cfg, profile)
	case "ema_trend":
		return f.buildEMATrend(cfg, profile)
	case "rsi_extreme":
		return f.buildRSI(cfg, profile)
	case "macd_trend":
		return f.buildMACD(cfg, profile)
	case "wt_mfi_hybrid":
		return f.buildWTMFIHybrid(cfg, profile)
	default:
		return nil, fmt.Errorf("unknown middleware: %s", cfg.Name)
	}
}

func (f *Factory) buildCandleFetcher(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	intervals := sliceFromCfg(cfg.Params, "intervals")
	if len(intervals) == 0 {
		intervals = profile.IntervalsLower()
	}
	if len(intervals) == 0 {
		return nil, fmt.Errorf("kline_fetcher 缺少 intervals")
	}
	limit := intFromCfg(cfg.Params, "limit")
	if limit <= 0 {
		if f.DefaultLimit > 0 {
			limit = f.DefaultLimit
		}
	}
	if limit <= 0 {
		return nil, fmt.Errorf("kline_fetcher 缺少有效的 limit")
	}
	mw := middlewares.NewCandleFetcher(middlewares.CandleFetcherConfig{
		Name:      cfg.Name,
		Stage:     cfg.Stage,
		Critical:  cfg.Critical,
		Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
		Intervals: intervals,
		Limit:     limit,
	}, f.Exporter)
	return mw, nil
}

func (f *Factory) buildEMATrend(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	interval := stringFromCfg(cfg.Params, "interval")
	if interval == "" {
		if ints := profile.IntervalsLower(); len(ints) > 0 {
			interval = ints[0]
		}
	}
	if interval == "" {
		return nil, fmt.Errorf("ema_trend 缺少 interval")
	}
	fast := intFromCfg(cfg.Params, "fast")
	mid := intFromCfg(cfg.Params, "mid")
	slow := intFromCfg(cfg.Params, "slow")
	if fast <= 0 || mid <= 0 || slow <= 0 {
		return nil, fmt.Errorf("ema_trend 需设置 fast/mid/slow")
	}
	mw := middlewares.NewEMATrend(middlewares.EMATrendConfig{
		Name:     cfg.Name,
		Stage:    cfg.Stage,
		Critical: cfg.Critical,
		Timeout:  time.Duration(cfg.TimeoutSeconds) * time.Second,
		Interval: interval,
		Fast:     fast,
		Mid:      mid,
		Slow:     slow,
	})
	return mw, nil
}

func (f *Factory) buildRSI(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	interval := stringFromCfg(cfg.Params, "interval")
	if interval == "" {
		ints := profile.IntervalsLower()
		if len(ints) > 0 {
			interval = ints[0]
		}
	}
	if interval == "" {
		return nil, fmt.Errorf("rsi_extreme 缺少 interval")
	}
	period := intFromCfg(cfg.Params, "period")
	if period <= 0 {
		return nil, fmt.Errorf("rsi_extreme 缺少 period")
	}
	overbought := floatFromCfg(cfg.Params, "overbought")
	if overbought == 0 {
		return nil, fmt.Errorf("rsi_extreme 缺少 overbought")
	}
	oversold := floatFromCfg(cfg.Params, "oversold")
	if oversold == 0 {
		return nil, fmt.Errorf("rsi_extreme 缺少 oversold")
	}
	mw := middlewares.NewRSIMiddleware(middlewares.RSIConfig{
		Name:       cfg.Name,
		Stage:      cfg.Stage,
		Critical:   cfg.Critical,
		Timeout:    time.Duration(cfg.TimeoutSeconds) * time.Second,
		Interval:   interval,
		Period:     period,
		Overbought: overbought,
		Oversold:   oversold,
	})
	return mw, nil
}

func (f *Factory) buildMACD(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	interval := stringFromCfg(cfg.Params, "interval")
	if interval == "" {
		ints := profile.IntervalsLower()
		if len(ints) > 0 {
			interval = ints[0]
		}
	}
	if interval == "" {
		return nil, fmt.Errorf("macd_trend 缺少 interval")
	}
	fast := intFromCfg(cfg.Params, "fast")
	slow := intFromCfg(cfg.Params, "slow")
	signal := intFromCfg(cfg.Params, "signal")
	if fast <= 0 {
		fast = 12
	}
	if slow <= 0 {
		slow = 26
	}
	if signal <= 0 {
		signal = 9
	}
	if fast >= slow {
		return nil, fmt.Errorf("macd_trend fast 需小于 slow")
	}
	mw := middlewares.NewMACDMiddleware(middlewares.MACDConfig{
		Name:     cfg.Name,
		Stage:    cfg.Stage,
		Critical: cfg.Critical,
		Timeout:  time.Duration(cfg.TimeoutSeconds) * time.Second,
		Interval: interval,
		Fast:     fast,
		Slow:     slow,
		Signal:   signal,
	})
	return mw, nil
}

func (f *Factory) buildWTMFIHybrid(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error) {
	interval := stringFromCfg(cfg.Params, "interval")
	if interval == "" {
		ints := profile.IntervalsLower()
		if len(ints) > 0 {
			interval = ints[0]
		}
	}
	if interval == "" {
		return nil, fmt.Errorf("wt_mfi_hybrid 缂哄皯 interval")
	}

	channelLen := intFromCfg(cfg.Params, "len")
	if channelLen <= 0 {
		channelLen = intFromCfg(cfg.Params, "channel_len")
	}
	avgLen := intFromCfg(cfg.Params, "avg_len")
	smoothLen := intFromCfg(cfg.Params, "smooth_len")
	mfiLen := intFromCfg(cfg.Params, "mfi_len")
	wtWeight := floatFromCfg(cfg.Params, "wt_weight")
	mfiScale := floatFromCfg(cfg.Params, "mfi_scale")
	overbought := floatFromCfg(cfg.Params, "overbought")
	oversold := floatFromCfg(cfg.Params, "oversold")
	volLen := intFromCfg(cfg.Params, "vol_len")
	volTrigger := floatFromCfg(cfg.Params, "vol_trigger")
	useRealtimeDiv := boolFromCfg(cfg.Params, "use_realtime_div", true)
	realtimeLookback := intFromCfg(cfg.Params, "realtime_lookback")
	realtimeTriggerFlip := boolFromCfg(cfg.Params, "realtime_trigger_on_flip", true)
	searchLimit := intFromCfg(cfg.Params, "search_limit")
	minDivInterval := intFromCfg(cfg.Params, "min_div_interval")
	divMinAbsOscStart := floatFromCfg(cfg.Params, "div_min_abs_osc_start")
	divMinAbsOscEnd := floatFromCfg(cfg.Params, "div_min_abs_osc_end")
	divMinOscGap := floatFromCfg(cfg.Params, "div_min_osc_gap")
	divMinPriceGapATR := floatFromCfg(cfg.Params, "div_min_price_gap_atr")
	useATRFilter := boolFromCfg(cfg.Params, "use_atr_filter", true)
	atrLen := intFromCfg(cfg.Params, "atr_len")
	atrDivMult := floatFromCfg(cfg.Params, "atr_div_mult")
	useADXFilter := boolFromCfg(cfg.Params, "use_adx_filter_div", true)
	adxLen := intFromCfg(cfg.Params, "adx_len")
	adxLimit := floatFromCfg(cfg.Params, "adx_limit")
	requireCandleConf := boolFromCfg(cfg.Params, "require_candle_conf", true)
	lbLeft := intFromCfg(cfg.Params, "lb_left")
	lbRight := intFromCfg(cfg.Params, "lb_right")

	mw := middlewares.NewWTMFIHybrid(middlewares.WTMFIHybridConfig{
		Name:                  cfg.Name,
		Stage:                 cfg.Stage,
		Critical:              cfg.Critical,
		Timeout:               time.Duration(cfg.TimeoutSeconds) * time.Second,
		Interval:              interval,
		ChannelLen:            channelLen,
		AvgLen:                avgLen,
		SmoothLen:             smoothLen,
		MFILen:                mfiLen,
		WTWeight:              wtWeight,
		MFIScale:              mfiScale,
		Overbought:            overbought,
		Oversold:              oversold,
		VolLen:                volLen,
		VolTrigger:            volTrigger,
		UseRealtimeDiv:        useRealtimeDiv,
		RealtimeLookback:      realtimeLookback,
		RealtimeTriggerOnFlip: realtimeTriggerFlip,
		SearchLimit:           searchLimit,
		MinDivInterval:        minDivInterval,
		DivMinAbsOscStart:     divMinAbsOscStart,
		DivMinAbsOscEnd:       divMinAbsOscEnd,
		DivMinOscGap:          divMinOscGap,
		DivMinPriceGapATR:     divMinPriceGapATR,
		UseATRFilter:          useATRFilter,
		ATRLen:                atrLen,
		ATRDivMult:            atrDivMult,
		UseADXFilter:          useADXFilter,
		ADXLen:                adxLen,
		ADXLimit:              adxLimit,
		RequireCandleConfirm:  requireCandleConf,
		LBLeft:                lbLeft,
		LBRight:               lbRight,
	})
	return mw, nil
}

func sliceFromCfg(params map[string]interface{}, key string) []string {
	if params == nil {
		return nil
	}
	raw, ok := params[key]
	if !ok {
		return nil
	}
	switch val := raw.(type) {
	case []string:
		return val
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			str := strings.TrimSpace(fmt.Sprintf("%v", item))
			if str == "" {
				continue
			}
			out = append(out, str)
		}
		return out
	default:
		parts := strings.Split(fmt.Sprintf("%v", val), ",")
		out := make([]string, 0, len(parts))
		for _, item := range parts {
			s := strings.TrimSpace(item)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
}

func stringFromCfg(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	raw, ok := params[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

func intFromCfg(params map[string]interface{}, key string) int {
	if params == nil {
		return 0
	}
	raw, ok := params[key]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		val, err := strconv.Atoi(fmt.Sprintf("%v", v))
		if err != nil {
			logger.Warnf("middleware param %s invalid int: %v", key, err)
			return 0
		}
		return val
	}
}

func floatFromCfg(params map[string]interface{}, key string) float64 {
	if params == nil {
		return 0
	}
	raw, ok := params[key]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		val, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
		if err != nil {
			logger.Warnf("middleware param %s invalid float: %v", key, err)
			return 0
		}
		return val
	}
}

func boolFromCfg(params map[string]interface{}, key string, fallback bool) bool {
	if params == nil {
		return fallback
	}
	raw, ok := params[key]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		val, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return fallback
		}
		return val
	default:
		val, err := strconv.ParseBool(strings.TrimSpace(fmt.Sprintf("%v", v)))
		if err != nil {
			return fallback
		}
		return val
	}
}
