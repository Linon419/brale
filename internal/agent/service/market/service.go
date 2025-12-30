package market

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"brale/internal/analysis/indicator"
	"brale/internal/agent/interfaces"
	"brale/internal/config"
	"brale/internal/decision"
	"brale/internal/market"
	"brale/internal/pkg/maputil"
	"brale/internal/profile"
	"brale/internal/store"
)

type Service struct {
	cfg        *config.Config
	ks         market.KlineStore
	profileMgr *profile.Manager

	monitor PriceSource

	indicatorMu   sync.RWMutex
	indicatorSnap map[string]indicatorSnapshot

	hIntervals  []string
	horizonName string
	visionReady bool
}

type PriceSource interface {
	LatestPrice(ctx context.Context, symbol string) float64
}

type ServiceParams struct {
	Config      *config.Config
	KlineStore  market.KlineStore
	ProfileMgr  *profile.Manager
	Monitor     PriceSource
	Intervals   []string
	HorizonName string
	VisionReady bool
}

func NewService(p ServiceParams) *Service {
	return &Service{
		cfg:           p.Config,
		ks:            p.KlineStore,
		profileMgr:    p.ProfileMgr,
		monitor:       p.Monitor,
		hIntervals:    p.Intervals,
		horizonName:   p.HorizonName,
		visionReady:   p.VisionReady,
		indicatorSnap: make(map[string]indicatorSnapshot),
	}
}

var _ interfaces.MarketService = (*Service)(nil)

type indicatorSnapshot struct {
	ATR       float64
	UpdatedAt time.Time
}

func (s *Service) GetAnalysisContexts(ctx context.Context, symbols []string) ([]decision.AnalysisContext, error) {
	exporter, ok := s.ks.(store.SnapshotExporter)
	if !ok || s.profileMgr == nil {
		return nil, nil
	}

	out := make([]decision.AnalysisContext, 0, len(symbols))
	for _, sym := range symbols {
		symbol := strings.ToUpper(strings.TrimSpace(sym))
		if symbol == "" {
			continue
		}
		rt, ok := s.profileMgr.Resolve(symbol)
		if !ok || rt == nil || rt.AnalysisSlice <= 0 {
			continue
		}

		intervals := rt.Definition.IntervalsLower()
		if len(intervals) == 0 {
			intervals = s.hIntervals
		}
		if len(intervals) == 0 {
			intervals = []string{"1h"}
		}

		emaByInterval := collectEMAByInterval(rt)
		wtmfiByInterval := collectWTMFIByInterval(rt)
		input := decision.AnalysisBuildInput{
			Context:           ctx,
			Exporter:          exporter,
			Symbols:           []string{symbol},
			Intervals:         intervals,
			Limit:             s.cfg.Kline.MaxCached,
			SliceLength:       rt.AnalysisSlice,
			SliceDrop:         rt.SliceDropTail,
			HorizonName:       s.horizonName,
			IndicatorLookback: rt.IndicatorBars,
			EMAByInterval:     emaByInterval,
			WTMFIByInterval:   wtmfiByInterval,
			WithImages:        s.visionReady,
			DisableIndicators: !rt.AgentEnabled,
			RequireATR:        profileNeedsATR(rt),
		}
		out = append(out, decision.BuildAnalysisContexts(input)...)
	}
	return out, nil
}

func collectEMAByInterval(rt *profile.Runtime) map[string]indicator.EMASettings {
	if rt == nil {
		return nil
	}
	middlewares := rt.Definition.Middlewares
	if len(middlewares) == 0 {
		return nil
	}
	out := make(map[string]indicator.EMASettings)
	for _, mw := range middlewares {
		if strings.ToLower(strings.TrimSpace(mw.Name)) != "ema_trend" {
			continue
		}
		interval := strings.ToLower(strings.TrimSpace(maputil.String(mw.Params, "interval")))
		if interval == "" {
			continue
		}
		settings := indicator.EMASettings{
			Fast: maputil.Int(mw.Params, "fast"),
			Mid:  maputil.Int(mw.Params, "mid"),
			Slow: maputil.Int(mw.Params, "slow"),
			Long: maputil.Int(mw.Params, "long"),
		}
		if settings.Fast <= 0 || settings.Mid <= 0 || settings.Slow <= 0 || settings.Long <= 0 {
			continue
		}
		out[interval] = settings
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectWTMFIByInterval(rt *profile.Runtime) map[string]indicator.WTMFISettings {
	if rt == nil {
		return nil
	}
	middlewares := rt.Definition.Middlewares
	if len(middlewares) == 0 {
		return nil
	}
	out := make(map[string]indicator.WTMFISettings)
	for _, mw := range middlewares {
		if strings.ToLower(strings.TrimSpace(mw.Name)) != "wt_mfi_hybrid" {
			continue
		}
		interval := strings.ToLower(strings.TrimSpace(maputil.String(mw.Params, "interval")))
		if interval == "" {
			continue
		}
		channelLen := maputil.Int(mw.Params, "len")
		if channelLen <= 0 {
			channelLen = maputil.Int(mw.Params, "channel_len")
		}
		settings := indicator.WTMFISettings{
			ChannelLen: channelLen,
			AvgLen:     maputil.Int(mw.Params, "avg_len"),
			SmoothLen:  maputil.Int(mw.Params, "smooth_len"),
			MFILen:     maputil.Int(mw.Params, "mfi_len"),
			WTWeight:   maputil.Float(mw.Params, "wt_weight"),
			MFIScale:   maputil.Float(mw.Params, "mfi_scale"),
			Overbought: maputil.Float(mw.Params, "overbought"),
			Oversold:   maputil.Float(mw.Params, "oversold"),
		}
		if settings.ChannelLen <= 0 && settings.AvgLen <= 0 && settings.SmoothLen <= 0 &&
			settings.MFILen <= 0 && settings.WTWeight <= 0 && settings.MFIScale <= 0 &&
			settings.Overbought == 0 && settings.Oversold == 0 {
			continue
		}
		out[interval] = settings
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Service) LatestPrice(ctx context.Context, symbol string) float64 {
	if s.monitor != nil {
		return s.monitor.LatestPrice(ctx, symbol)
	}
	return 0
}

func (s *Service) CaptureIndicators(ctxs []decision.AnalysisContext) {
	if len(ctxs) == 0 {
		return
	}
	now := time.Now()
	s.indicatorMu.Lock()
	defer s.indicatorMu.Unlock()

	for _, c := range ctxs {
		if strings.TrimSpace(c.IndicatorJSON) == "" {
			continue
		}
		var payload struct {
			Data struct {
				ATR *struct {
					Latest float64 `json:"latest"`
				} `json:"atr"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(c.IndicatorJSON), &payload); err != nil {
			continue
		}
		atr := 0.0
		if payload.Data.ATR != nil {
			atr = payload.Data.ATR.Latest
		}
		if atr != 0 {
			sym := strings.ToUpper(strings.TrimSpace(c.Symbol))
			s.indicatorSnap[sym] = indicatorSnapshot{
				ATR:       atr,
				UpdatedAt: now,
			}
		}
	}
}

func (s *Service) GetATR(symbol string) (float64, bool) {
	s.indicatorMu.RLock()
	defer s.indicatorMu.RUnlock()
	snap, ok := s.indicatorSnap[strings.ToUpper(symbol)]
	if !ok {
		return 0, false
	}
	if time.Since(snap.UpdatedAt) > 30*time.Minute {
		return 0, false
	}
	return snap.ATR, true
}

func profileNeedsATR(rt *profile.Runtime) bool {
	if rt == nil {
		return false
	}
	binding := rt.Definition.ExitPlans
	for _, id := range binding.Allowed {
		if containsATRKeyword(id) {
			return true
		}
	}
	for _, combo := range binding.Combos {
		if containsATRKeyword(combo) {
			return true
		}
	}
	return false
}

func containsATRKeyword(val string) bool {
	val = strings.ToLower(strings.TrimSpace(val))
	return strings.Contains(val, "atr")
}

// GetCandles returns candles for a symbol and interval. Used for ATR-based leverage calculation.
func (s *Service) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]market.Candle, error) {
	if s.ks == nil {
		return nil, nil
	}
	exporter, ok := s.ks.(store.SnapshotExporter)
	if !ok {
		return nil, nil
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	iv := strings.ToLower(strings.TrimSpace(interval))
	if sym == "" || iv == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	return exporter.Export(ctx, sym, iv, limit)
}
