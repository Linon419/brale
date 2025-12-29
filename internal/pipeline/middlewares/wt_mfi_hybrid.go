package middlewares

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"brale/internal/analysis/indicator"
	"brale/internal/market"
	"brale/internal/pipeline"

	talib "github.com/markcheno/go-talib"
)

type WTMFIHybridConfig struct {
	Name       string
	Stage      int
	Critical   bool
	Timeout    time.Duration
	Interval   string
	ChannelLen int
	AvgLen     int
	SmoothLen  int
	MFILen     int
	WTWeight   float64
	MFIScale   float64
	Overbought float64
	Oversold   float64
	VolLen     int
	VolTrigger float64

	UseRealtimeDiv        bool
	RealtimeLookback      int
	RealtimeTriggerOnFlip bool
	SearchLimit           int
	MinDivInterval        int
	DivMinAbsOscStart     float64
	DivMinAbsOscEnd       float64
	DivMinOscGap          float64
	DivMinPriceGapATR     float64
	UseATRFilter          bool
	ATRLen                int
	ATRDivMult            float64
	UseADXFilter          bool
	ADXLen                int
	ADXLimit              float64
	RequireCandleConfirm  bool
	LBLeft                int
	LBRight               int
}

type WTMFIHybridMiddleware struct {
	meta       pipeline.MiddlewareMeta
	interval   string
	channelLen int
	avgLen     int
	smoothLen  int
	mfiLen     int
	wtWeight   float64
	mfiScale   float64
	overbought float64
	oversold   float64
	volLen     int
	volTrigger float64

	useRealtimeDiv        bool
	realtimeLookback      int
	realtimeTriggerOnFlip bool
	searchLimit           int
	minDivInterval        int
	divMinAbsOscStart     float64
	divMinAbsOscEnd       float64
	divMinOscGap          float64
	divMinPriceGapATR     float64
	useATRFilter          bool
	atrLen                int
	atrDivMult            float64
	useADXFilter          bool
	adxLen                int
	adxLimit              float64
	requireCandleConfirm  bool
	lbLeft                int
	lbRight               int
}

func NewWTMFIHybrid(cfg WTMFIHybridConfig) *WTMFIHybridMiddleware {
	if cfg.ChannelLen <= 0 {
		cfg.ChannelLen = 10
	}
	if cfg.AvgLen <= 0 {
		cfg.AvgLen = 8
	}
	if cfg.SmoothLen <= 0 {
		cfg.SmoothLen = 5
	}
	if cfg.MFILen <= 0 {
		cfg.MFILen = 10
	}
	if cfg.WTWeight <= 0 {
		cfg.WTWeight = 0.3
	} else if cfg.WTWeight > 1 {
		cfg.WTWeight = 1
	}
	if cfg.MFIScale <= 0 {
		cfg.MFIScale = 1.5
	}
	if cfg.Overbought == 0 {
		cfg.Overbought = 50
	}
	if cfg.Oversold == 0 {
		cfg.Oversold = -50
	}
	if cfg.VolLen <= 0 {
		cfg.VolLen = 60
	}
	if cfg.VolTrigger <= 0 {
		cfg.VolTrigger = 2.0
	}
	if cfg.RealtimeLookback <= 0 {
		cfg.RealtimeLookback = 3
	}
	if cfg.SearchLimit <= 0 {
		cfg.SearchLimit = 65
	}
	if cfg.MinDivInterval <= 0 {
		cfg.MinDivInterval = 5
	}
	if cfg.ATRLen <= 0 {
		cfg.ATRLen = 14
	}
	if cfg.ATRDivMult <= 0 {
		cfg.ATRDivMult = 0.3
	}
	if cfg.ADXLen <= 0 {
		cfg.ADXLen = 14
	}
	if cfg.ADXLimit <= 0 {
		cfg.ADXLimit = 35
	}
	if cfg.LBLeft <= 0 {
		cfg.LBLeft = 5
	}
	if cfg.LBRight <= 0 {
		cfg.LBRight = 3
	}
	return &WTMFIHybridMiddleware{
		meta: pipeline.MiddlewareMeta{
			Name:     nameOrDefault(cfg.Name, "wt_mfi_hybrid"),
			Stage:    cfg.Stage,
			Critical: cfg.Critical,
			Timeout:  cfg.Timeout,
		},
		interval:              strings.ToLower(strings.TrimSpace(cfg.Interval)),
		channelLen:            cfg.ChannelLen,
		avgLen:                cfg.AvgLen,
		smoothLen:             cfg.SmoothLen,
		mfiLen:                cfg.MFILen,
		wtWeight:              cfg.WTWeight,
		mfiScale:              cfg.MFIScale,
		overbought:            cfg.Overbought,
		oversold:              cfg.Oversold,
		volLen:                cfg.VolLen,
		volTrigger:            cfg.VolTrigger,
		useRealtimeDiv:        cfg.UseRealtimeDiv,
		realtimeLookback:      cfg.RealtimeLookback,
		realtimeTriggerOnFlip: cfg.RealtimeTriggerOnFlip,
		searchLimit:           cfg.SearchLimit,
		minDivInterval:        cfg.MinDivInterval,
		divMinAbsOscStart:     cfg.DivMinAbsOscStart,
		divMinAbsOscEnd:       cfg.DivMinAbsOscEnd,
		divMinOscGap:          cfg.DivMinOscGap,
		divMinPriceGapATR:     cfg.DivMinPriceGapATR,
		useATRFilter:          cfg.UseATRFilter,
		atrLen:                cfg.ATRLen,
		atrDivMult:            cfg.ATRDivMult,
		useADXFilter:          cfg.UseADXFilter,
		adxLen:                cfg.ADXLen,
		adxLimit:              cfg.ADXLimit,
		requireCandleConfirm:  cfg.RequireCandleConfirm,
		lbLeft:                cfg.LBLeft,
		lbRight:               cfg.LBRight,
	}
}

func (m *WTMFIHybridMiddleware) Meta() pipeline.MiddlewareMeta { return m.meta }

func (m *WTMFIHybridMiddleware) Handle(ctx context.Context, ac *pipeline.AnalysisContext) error {
	interval := m.interval
	if interval == "" {
		interval = "1h"
	}
	candles := ac.Candles(interval)
	required := maxInt(m.channelLen, m.avgLen, m.smoothLen, m.mfiLen) + 1
	if len(candles) < required {
		return fmt.Errorf("wt_mfi_hybrid: insufficient candles %s need %d got %d", interval, required, len(candles))
	}

	opens, src, highs, lows, closes, volumes := buildSeries(candles)

	esa := talib.Ema(src, m.channelLen)
	absDiff := make([]float64, len(src))
	for i := range src {
		absDiff[i] = math.Abs(src[i] - esa[i])
	}
	d := talib.Ema(absDiff, m.channelLen)
	ci := make([]float64, len(src))
	for i := range src {
		denom := 0.015 * d[i]
		if denom == 0 {
			ci[i] = 0
			continue
		}
		ci[i] = (src[i] - esa[i]) / denom
	}
	wt1 := talib.Ema(ci, m.avgLen)
	wt2 := almaSeries(wt1, m.smoothLen, 0.85, 6)

	mfiSeries := talib.Mfi(highs, lows, closes, volumes, m.mfiLen)
	hybrid := make([]float64, len(src))
	for i := range hybrid {
		mfiVal := (mfiSeries[i] - 50) * m.mfiScale
		hybrid[i] = m.wtWeight*wt2[i] + (1-m.wtWeight)*mfiVal
	}

	osc := indicator.WTMFIPostProcess(hybrid, m.smoothLen)

	volZ := volumeZScore(volumes, m.volLen)
	atr := talib.Atr(highs, lows, closes, m.atrLen)
	adx := talib.Adx(highs, lows, closes, m.adxLen)

	divBull, divBear := detectDivergences(divergenceInput{
		osc:                  osc,
		opens:                opens,
		highs:                highs,
		lows:                 lows,
		closes:               closes,
		atr:                  atr,
		volZ:                 volZ,
		adx:                  adx,
		candles:              candles,
		useRealtime:          m.useRealtimeDiv,
		realtimeLookback:     m.realtimeLookback,
		realtimeTriggerFlip:  m.realtimeTriggerOnFlip,
		searchLimit:          m.searchLimit,
		minDivInterval:       m.minDivInterval,
		divMinAbsOscStart:    m.divMinAbsOscStart,
		divMinAbsOscEnd:      m.divMinAbsOscEnd,
		divMinOscGap:         m.divMinOscGap,
		divMinPriceGapATR:    m.divMinPriceGapATR,
		useATRFilter:         m.useATRFilter,
		atrDivMult:           m.atrDivMult,
		useADXFilter:         m.useADXFilter,
		adxLimit:             m.adxLimit,
		requireCandleConfirm: m.requireCandleConfirm,
		lbLeft:               m.lbLeft,
		lbRight:              m.lbRight,
		volTrigger:           m.volTrigger,
	})

	idx := len(osc) - 1
	latest := osc[idx]
	status := "NEUTRAL"
	if latest >= m.overbought {
		status = "OVERBOUGHT"
	} else if latest <= m.oversold {
		status = "OVERSOLD"
	}
	desc := fmt.Sprintf("WT+MFI hybrid %s len=%d avg=%d smooth=%d mfi=%d wt=%.2f latest=%.2f",
		strings.ToUpper(interval), m.channelLen, m.avgLen, m.smoothLen, m.mfiLen, m.wtWeight, latest)

	ac.AddFeature(pipeline.Feature{
		Key:         "wt_mfi_hybrid",
		Label:       fmt.Sprintf("%s WT+MFI", strings.ToUpper(interval)),
		Value:       latest,
		Description: formatFeature(ac.Symbol, desc),
		Metadata: map[string]any{
			"interval":     interval,
			"channel_len":  m.channelLen,
			"avg_len":      m.avgLen,
			"smooth_len":   m.smoothLen,
			"mfi_len":      m.mfiLen,
			"wt_weight":    m.wtWeight,
			"mfi_scale":    m.mfiScale,
			"overbought":   m.overbought,
			"oversold":     m.oversold,
			"vol_len":      m.volLen,
			"vol_trigger":  m.volTrigger,
			"status":       status,
			"latest_value": latest,
			"latest_time":  candleTimeUTC(candles, idx),
			"series_tail":  seriesTail(osc, 5),
			"volume_zscore": func() float64 {
				if len(volZ) == 0 {
					return 0
				}
				return volZ[idx]
			}(),
			"m_plus": func() bool {
				if len(volZ) == 0 {
					return false
				}
				return volZ[idx] > m.volTrigger
			}(),
			"divergence": map[string]any{
				"bull": divBull.toMap(),
				"bear": divBear.toMap(),
			},
		},
	})
	return nil
}

func buildSeries(candles []market.Candle) (opens, src, highs, lows, closes, volumes []float64) {
	n := len(candles)
	opens = make([]float64, n)
	src = make([]float64, n)
	highs = make([]float64, n)
	lows = make([]float64, n)
	closes = make([]float64, n)
	volumes = make([]float64, n)
	for i, c := range candles {
		opens[i] = c.Open
		highs[i] = c.High
		lows[i] = c.Low
		closes[i] = c.Close
		volumes[i] = c.Volume
		src[i] = (c.High + c.Low + c.Close) / 3
	}
	return opens, src, highs, lows, closes, volumes
}

func almaSeries(values []float64, length int, offset, sigma float64) []float64 {
	out := make([]float64, len(values))
	if length <= 0 || len(values) == 0 {
		return out
	}
	m := offset * float64(length-1)
	s := float64(length) / sigma
	denom := 2 * s * s
	for i := range values {
		if i+1 < length {
			out[i] = 0
			continue
		}
		sum := 0.0
		wSum := 0.0
		for j := 0; j < length; j++ {
			idx := i - length + 1 + j
			w := math.Exp(-((float64(j) - m) * (float64(j) - m)) / denom)
			sum += w * values[idx]
			wSum += w
		}
		if wSum == 0 {
			out[i] = 0
		} else {
			out[i] = sum / wSum
		}
	}
	return out
}

func candleTimeUTC(candles []market.Candle, idx int) string {
	if idx < 0 || idx >= len(candles) {
		return "n/a"
	}
	ts := candles[idx].CloseTime
	if ts == 0 {
		ts = candles[idx].OpenTime
	}
	if ts == 0 {
		return "n/a"
	}
	return time.UnixMilli(ts).UTC().Format(time.RFC3339)
}

type divergenceInput struct {
	osc                  []float64
	opens                []float64
	highs                []float64
	lows                 []float64
	closes               []float64
	atr                  []float64
	volZ                 []float64
	adx                  []float64
	candles              []market.Candle
	useRealtime          bool
	realtimeLookback     int
	realtimeTriggerFlip  bool
	searchLimit          int
	minDivInterval       int
	divMinAbsOscStart    float64
	divMinAbsOscEnd      float64
	divMinOscGap         float64
	divMinPriceGapATR    float64
	useATRFilter         bool
	atrDivMult           float64
	useADXFilter         bool
	adxLimit             float64
	requireCandleConfirm bool
	lbLeft               int
	lbRight              int
	volTrigger           float64
}

type divergenceSignal struct {
	signal   bool
	mPlus    bool
	level    int
	distance int
	sequence int
	oscGap   float64
	priceGap float64
	time     string
}

func (d divergenceSignal) toMap() map[string]any {
	return map[string]any{
		"signal":    d.signal,
		"m_plus":    d.mPlus,
		"level":     d.level,
		"distance":  d.distance,
		"sequence":  d.sequence,
		"osc_gap":   d.oscGap,
		"price_gap": d.priceGap,
		"time":      d.time,
	}
}

func detectDivergences(in divergenceInput) (divergenceSignal, divergenceSignal) {
	n := len(in.osc)
	if n == 0 {
		return divergenceSignal{}, divergenceSignal{}
	}
	if in.realtimeLookback <= 0 {
		in.realtimeLookback = 3
	}
	if in.searchLimit <= 0 {
		in.searchLimit = 65
	}
	if in.minDivInterval <= 0 {
		in.minDivInterval = 5
	}
	if in.atrDivMult <= 0 {
		in.atrDivMult = 0.3
	}
	if in.lbLeft <= 0 {
		in.lbLeft = 5
	}
	if in.lbRight <= 0 {
		in.lbRight = 3
	}

	if in.useRealtime {
		return detectRealtimeDivergence(in)
	}
	return detectPivotDivergence(in)
}

func detectRealtimeDivergence(in divergenceInput) (divergenceSignal, divergenceSignal) {
	n := len(in.osc)
	lastBullIdx := -100000
	lastBearIdx := -100000
	lastBullSignalIdx := -1
	lastBearSignalIdx := -1
	bullSeq := 0
	bearSeq := 0
	var lastBull divergenceSignal
	var lastBear divergenceSignal
	prevIsPeak := false
	prevIsBottom := false

	for idx := 0; idx < n; idx++ {
		if idx < in.searchLimit+in.realtimeLookback+2 {
			prevIsPeak = isRealtimePeak(in.osc, idx, in.realtimeLookback, in.divMinAbsOscEnd)
			prevIsBottom = isRealtimeBottom(in.osc, idx, in.realtimeLookback, in.divMinAbsOscEnd)
			continue
		}

		isPeak := isRealtimePeak(in.osc, idx, in.realtimeLookback, in.divMinAbsOscEnd)
		isBottom := isRealtimeBottom(in.osc, idx, in.realtimeLookback, in.divMinAbsOscEnd)
		peakEvent := isPeak
		bottomEvent := isBottom
		if in.realtimeTriggerFlip {
			peakEvent = isPeak && !prevIsPeak
			bottomEvent = isBottom && !prevIsBottom
		}
		prevIsPeak = isPeak
		prevIsBottom = isBottom

		if peakEvent {
			bearIntervalOK := idx-lastBearIdx >= in.minDivInterval
			if bearIntervalOK && allowByADX(in.adx, idx, in.useADXFilter, in.adxLimit) &&
				candleConfirmBear(in.opens, in.closes, idx, in.requireCandleConfirm) &&
				priceMovedUp(in.highs, in.lows, in.atr, idx, in.realtimeLookback, in.atrDivMult, in.useATRFilter) {
				if sig, ok := scanBearDivergenceRealtime(in, idx); ok {
					if idx-lastBearIdx > in.searchLimit {
						bearSeq = 1
					} else {
						bearSeq++
					}
					lastBearIdx = idx
					lastBearSignalIdx = idx
					sig.sequence = bearSeq
					lastBear = sig
				}
			}
		}

		if bottomEvent {
			bullIntervalOK := idx-lastBullIdx >= in.minDivInterval
			if bullIntervalOK && allowByADX(in.adx, idx, in.useADXFilter, in.adxLimit) &&
				candleConfirmBull(in.opens, in.closes, idx, in.requireCandleConfirm) &&
				priceMovedDown(in.highs, in.lows, in.atr, idx, in.realtimeLookback, in.atrDivMult, in.useATRFilter) {
				if sig, ok := scanBullDivergenceRealtime(in, idx); ok {
					if idx-lastBullIdx > in.searchLimit {
						bullSeq = 1
					} else {
						bullSeq++
					}
					lastBullIdx = idx
					lastBullSignalIdx = idx
					sig.sequence = bullSeq
					lastBull = sig
				}
			}
		}
	}

	lastIdx := n - 1
	if lastBull.signal && lastBull.distance > 0 && lastBullSignalIdx == lastIdx {
		lastBull.signal = true
	} else {
		lastBull.signal = false
	}
	if lastBear.signal && lastBear.distance > 0 && lastBearSignalIdx == lastIdx {
		lastBear.signal = true
	} else {
		lastBear.signal = false
	}
	return lastBull, lastBear
}

func detectPivotDivergence(in divergenceInput) (divergenceSignal, divergenceSignal) {
	n := len(in.osc)
	lastBullIdx := -100000
	lastBearIdx := -100000
	lastBullSignalIdx := -1
	lastBearSignalIdx := -1
	bullSeq := 0
	bearSeq := 0
	var lastBull divergenceSignal
	var lastBear divergenceSignal

	for idx := in.lbLeft + in.lbRight; idx < n; idx++ {
		pivotIdx := idx - in.lbRight
		allowADX := allowByADX(in.adx, pivotIdx, in.useADXFilter, in.adxLimit)
		if !allowADX {
			continue
		}
		if isPivotHigh(in.osc, pivotIdx, in.lbLeft, in.lbRight) {
			if candleConfirmBear(in.opens, in.closes, pivotIdx, in.requireCandleConfirm) {
				if sig, ok := scanBearDivergencePivot(in, pivotIdx); ok {
					if pivotIdx-lastBearIdx > in.searchLimit {
						bearSeq = 1
					} else {
						bearSeq++
					}
					lastBearIdx = pivotIdx
					lastBearSignalIdx = idx
					sig.sequence = bearSeq
					lastBear = sig
				}
			}
		}
		if isPivotLow(in.osc, pivotIdx, in.lbLeft, in.lbRight) {
			if candleConfirmBull(in.opens, in.closes, pivotIdx, in.requireCandleConfirm) {
				if sig, ok := scanBullDivergencePivot(in, pivotIdx); ok {
					if pivotIdx-lastBullIdx > in.searchLimit {
						bullSeq = 1
					} else {
						bullSeq++
					}
					lastBullIdx = pivotIdx
					lastBullSignalIdx = idx
					sig.sequence = bullSeq
					lastBull = sig
				}
			}
		}
	}
	lastIdx := n - 1
	if lastBull.signal && lastBull.distance > 0 && lastBullSignalIdx == lastIdx {
		lastBull.signal = true
	} else {
		lastBull.signal = false
	}
	if lastBear.signal && lastBear.distance > 0 && lastBearSignalIdx == lastIdx {
		lastBear.signal = true
	} else {
		lastBear.signal = false
	}
	return lastBull, lastBear
}

func scanBearDivergenceRealtime(in divergenceInput, idx int) (divergenceSignal, bool) {
	currOsc := in.osc[idx]
	if currOsc <= 0 {
		return divergenceSignal{}, false
	}
	currPrice := in.highs[idx]
	divPriceGap := atrGap(in.atr, idx, in.divMinPriceGapATR)
	for i := in.realtimeLookback + 1; i <= in.searchLimit; i++ {
		pastIdx := idx - i
		if pastIdx <= 0 {
			break
		}
		checkOsc := in.osc[pastIdx]
		if !isLocalPeak(in.osc, pastIdx) {
			continue
		}
		if checkOsc <= 0 || checkOsc < in.divMinAbsOscStart {
			continue
		}
		if currPrice > in.highs[pastIdx] &&
			(checkOsc-currOsc) >= in.divMinOscGap &&
			(currPrice-in.highs[pastIdx]) >= divPriceGap {
			return divergenceSignal{
				signal:   true,
				mPlus:    isMPlus(in.volZ, idx, in.volTrigger),
				level:    divLevel(i),
				distance: i,
				oscGap:   checkOsc - currOsc,
				priceGap: currPrice - in.highs[pastIdx],
				time:     candleTimeUTC(in.candles, idx),
			}, true
		}
	}
	return divergenceSignal{}, false
}

func scanBullDivergenceRealtime(in divergenceInput, idx int) (divergenceSignal, bool) {
	currOsc := in.osc[idx]
	if currOsc >= 0 {
		return divergenceSignal{}, false
	}
	currPrice := in.lows[idx]
	divPriceGap := atrGap(in.atr, idx, in.divMinPriceGapATR)
	for i := in.realtimeLookback + 1; i <= in.searchLimit; i++ {
		pastIdx := idx - i
		if pastIdx <= 0 {
			break
		}
		checkOsc := in.osc[pastIdx]
		if !isLocalBottom(in.osc, pastIdx) {
			continue
		}
		if checkOsc >= 0 || checkOsc > -in.divMinAbsOscStart {
			continue
		}
		if currPrice < in.lows[pastIdx] &&
			(currOsc-checkOsc) >= in.divMinOscGap &&
			(in.lows[pastIdx]-currPrice) >= divPriceGap {
			return divergenceSignal{
				signal:   true,
				mPlus:    isMPlus(in.volZ, idx, in.volTrigger),
				level:    divLevel(i),
				distance: i,
				oscGap:   currOsc - checkOsc,
				priceGap: in.lows[pastIdx] - currPrice,
				time:     candleTimeUTC(in.candles, idx),
			}, true
		}
	}
	return divergenceSignal{}, false
}

func scanBearDivergencePivot(in divergenceInput, pivotIdx int) (divergenceSignal, bool) {
	currOsc := in.osc[pivotIdx]
	if currOsc <= 0 {
		return divergenceSignal{}, false
	}
	currPrice := in.highs[pivotIdx]
	divPriceGap := atrGap(in.atr, pivotIdx, in.divMinPriceGapATR)
	for i := 1; i <= in.searchLimit; i++ {
		pastIdx := pivotIdx - i
		if pastIdx <= 0 {
			break
		}
		checkOsc := in.osc[pastIdx]
		if !isLocalPeak(in.osc, pastIdx) {
			continue
		}
		if checkOsc <= 0 || checkOsc < in.divMinAbsOscStart {
			continue
		}
		if currPrice > in.highs[pastIdx] &&
			(checkOsc-currOsc) >= in.divMinOscGap &&
			(currPrice-in.highs[pastIdx]) >= divPriceGap {
			return divergenceSignal{
				signal:   true,
				mPlus:    isMPlus(in.volZ, pivotIdx, in.volTrigger),
				level:    divLevel(i),
				distance: i,
				oscGap:   checkOsc - currOsc,
				priceGap: currPrice - in.highs[pastIdx],
				time:     candleTimeUTC(in.candles, pivotIdx),
			}, true
		}
	}
	return divergenceSignal{}, false
}

func scanBullDivergencePivot(in divergenceInput, pivotIdx int) (divergenceSignal, bool) {
	currOsc := in.osc[pivotIdx]
	if currOsc >= 0 {
		return divergenceSignal{}, false
	}
	currPrice := in.lows[pivotIdx]
	divPriceGap := atrGap(in.atr, pivotIdx, in.divMinPriceGapATR)
	for i := 1; i <= in.searchLimit; i++ {
		pastIdx := pivotIdx - i
		if pastIdx <= 0 {
			break
		}
		checkOsc := in.osc[pastIdx]
		if !isLocalBottom(in.osc, pastIdx) {
			continue
		}
		if checkOsc >= 0 || checkOsc > -in.divMinAbsOscStart {
			continue
		}
		if currPrice < in.lows[pastIdx] &&
			(currOsc-checkOsc) >= in.divMinOscGap &&
			(in.lows[pastIdx]-currPrice) >= divPriceGap {
			return divergenceSignal{
				signal:   true,
				mPlus:    isMPlus(in.volZ, pivotIdx, in.volTrigger),
				level:    divLevel(i),
				distance: i,
				oscGap:   currOsc - checkOsc,
				priceGap: in.lows[pastIdx] - currPrice,
				time:     candleTimeUTC(in.candles, pivotIdx),
			}, true
		}
	}
	return divergenceSignal{}, false
}

func isRealtimePeak(osc []float64, idx, lookback int, minAbsEnd float64) bool {
	if idx < 0 || idx >= len(osc) {
		return false
	}
	val := osc[idx]
	if val <= 0 || val < minAbsEnd {
		return false
	}
	return val >= windowMax(osc, idx, lookback)
}

func isRealtimeBottom(osc []float64, idx, lookback int, minAbsEnd float64) bool {
	if idx < 0 || idx >= len(osc) {
		return false
	}
	val := osc[idx]
	if val >= 0 || val > -minAbsEnd {
		return false
	}
	return val <= windowMin(osc, idx, lookback)
}

func isLocalPeak(osc []float64, idx int) bool {
	if idx <= 0 || idx >= len(osc)-1 {
		return false
	}
	return osc[idx] >= osc[idx-1] && osc[idx] >= osc[idx+1]
}

func isLocalBottom(osc []float64, idx int) bool {
	if idx <= 0 || idx >= len(osc)-1 {
		return false
	}
	return osc[idx] <= osc[idx-1] && osc[idx] <= osc[idx+1]
}

func isPivotHigh(osc []float64, pivotIdx, left, right int) bool {
	if pivotIdx-left < 0 || pivotIdx+right >= len(osc) {
		return false
	}
	val := osc[pivotIdx]
	for i := pivotIdx - left; i <= pivotIdx+right; i++ {
		if osc[i] > val {
			return false
		}
	}
	return true
}

func isPivotLow(osc []float64, pivotIdx, left, right int) bool {
	if pivotIdx-left < 0 || pivotIdx+right >= len(osc) {
		return false
	}
	val := osc[pivotIdx]
	for i := pivotIdx - left; i <= pivotIdx+right; i++ {
		if osc[i] < val {
			return false
		}
	}
	return true
}

func windowMax(series []float64, idx, lookback int) float64 {
	if lookback <= 0 {
		return series[idx]
	}
	start := idx - lookback + 1
	if start < 0 {
		start = 0
	}
	maxVal := series[start]
	for i := start + 1; i <= idx; i++ {
		if series[i] > maxVal {
			maxVal = series[i]
		}
	}
	return maxVal
}

func windowMin(series []float64, idx, lookback int) float64 {
	if lookback <= 0 {
		return series[idx]
	}
	start := idx - lookback + 1
	if start < 0 {
		start = 0
	}
	minVal := series[start]
	for i := start + 1; i <= idx; i++ {
		if series[i] < minVal {
			minVal = series[i]
		}
	}
	return minVal
}

func priceMovedUp(highs, lows, atr []float64, idx, lookback int, atrMult float64, useATRFilter bool) bool {
	if !useATRFilter {
		return true
	}
	if idx < 0 || idx >= len(highs) {
		return false
	}
	minLow := windowMin(lows, idx, lookback)
	threshold := atrValue(atr, idx) * atrMult
	return highs[idx]-minLow > threshold
}

func priceMovedDown(highs, lows, atr []float64, idx, lookback int, atrMult float64, useATRFilter bool) bool {
	if !useATRFilter {
		return true
	}
	if idx < 0 || idx >= len(lows) {
		return false
	}
	maxHigh := windowMax(highs, idx, lookback)
	threshold := atrValue(atr, idx) * atrMult
	return maxHigh-lows[idx] > threshold
}

func atrValue(atr []float64, idx int) float64 {
	if idx < 0 || idx >= len(atr) {
		return 0
	}
	return atr[idx]
}

func atrGap(atr []float64, idx int, mult float64) float64 {
	if mult <= 0 {
		return 0
	}
	return atrValue(atr, idx) * mult
}

func allowByADX(adx []float64, idx int, useFilter bool, limit float64) bool {
	if !useFilter {
		return true
	}
	if idx < 0 || idx >= len(adx) {
		return true
	}
	return adx[idx] <= limit
}

func candleConfirmBear(opens, closes []float64, idx int, require bool) bool {
	if !require {
		return true
	}
	if idx < 0 || idx >= len(opens) {
		return false
	}
	return closes[idx] < opens[idx]
}

func candleConfirmBull(opens, closes []float64, idx int, require bool) bool {
	if !require {
		return true
	}
	if idx < 0 || idx >= len(opens) {
		return false
	}
	return closes[idx] >= opens[idx]
}

func divLevel(distance int) int {
	switch {
	case distance <= 15:
		return 1
	case distance <= 35:
		return 2
	case distance <= 55:
		return 3
	default:
		return 4
	}
}

func isMPlus(volZ []float64, idx int, trigger float64) bool {
	if idx < 0 || idx >= len(volZ) {
		return false
	}
	return volZ[idx] > trigger
}

func volumeZScore(volume []float64, length int) []float64 {
	out := make([]float64, len(volume))
	if len(volume) == 0 || length <= 1 {
		return out
	}
	ma := talib.Sma(volume, length)
	std := talib.StdDev(volume, length, 1.0)
	for i := range volume {
		if std[i] == 0 {
			out[i] = 0
			continue
		}
		out[i] = (volume[i] - ma[i]) / std[i]
	}
	return out
}

func maxInt(values ...int) int {
	max := 0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}
