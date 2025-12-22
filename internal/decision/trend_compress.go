package decision

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"brale/internal/market"

	"github.com/markcheno/go-talib"
)

type TrendCompressOptions struct {
	FractalSpan         int
	MaxStructurePoints  int
	DedupDistanceBars   int
	DedupATRFactor      float64
	RSIPeriod           int
	ATRPeriod           int
	RecentCandles       int
	VolumeMAPeriod      int
	EMA20Period         int
	EMA50Period         int
	EMA200Period        int
	Pretty              bool
	IncludeCurrentRSI   bool
	IncludeStructureRSI bool
}

func DefaultTrendCompressOptions() TrendCompressOptions {
	return TrendCompressOptions{
		FractalSpan:         2,
		MaxStructurePoints:  8,
		DedupDistanceBars:   10,
		DedupATRFactor:      0.5,
		RSIPeriod:           14,
		ATRPeriod:           14,
		RecentCandles:       7,
		VolumeMAPeriod:      20,
		EMA20Period:         20,
		EMA50Period:         50,
		EMA200Period:        200,
		Pretty:              false,
		IncludeCurrentRSI:   true,
		IncludeStructureRSI: true,
	}
}

type TrendCompressedInput struct {
	Meta                TrendCompressedMeta       `json:"meta"`
	StructurePoints     []TrendStructurePoint     `json:"structure_points"`
	StructureCandidates []TrendStructureCandidate `json:"structure_candidates,omitempty"`
	RecentCandles       []TrendRecentCandle       `json:"recent_candles"`
	GlobalContext       TrendGlobalContext        `json:"global_context"`
	RawCandles          []TrendRawCandleOptional  `json:"raw_candles,omitempty"`
}

type TrendCompressedMeta struct {
	Symbol    string `json:"symbol"`
	Interval  string `json:"interval"`
	Timestamp string `json:"timestamp"`
}

type TrendStructurePoint struct {
	Idx   int      `json:"idx"`
	Type  string   `json:"type"`
	Price float64  `json:"price"`
	RSI   *float64 `json:"rsi,omitempty"`
}

type TrendRecentCandle struct {
	Idx int      `json:"idx"`
	O   float64  `json:"o"`
	H   float64  `json:"h"`
	L   float64  `json:"l"`
	C   float64  `json:"c"`
	V   float64  `json:"v"`
	RSI *float64 `json:"rsi,omitempty"`
}

type TrendGlobalContext struct {
	TrendSlope      float64  `json:"trend_slope"`
	NormalizedSlope float64  `json:"normalized_slope"`
	SlopeState      string   `json:"slope_state,omitempty"`
	Window          int      `json:"window,omitempty"`
	VolRatio        float64  `json:"vol_ratio"`
	EMA20           *float64 `json:"ema20,omitempty"`
	EMA50           *float64 `json:"ema50,omitempty"`
	EMA200          *float64 `json:"ema200,omitempty"`
}

type TrendRawCandleOptional struct {
	Idx int     `json:"idx"`
	O   float64 `json:"o"`
	H   float64 `json:"h"`
	L   float64 `json:"l"`
	C   float64 `json:"c"`
	V   float64 `json:"v"`
}

type TrendStructureCandidate struct {
	Price      float64 `json:"price"`
	Type       string  `json:"type"`
	Source     string  `json:"source"`
	AgeCandles int     `json:"age_candles"`
	Window     int     `json:"window,omitempty"`
}

func BuildTrendCompressedJSON(symbol, interval string, candles []market.Candle, opts TrendCompressOptions) (string, error) {
	payload, err := BuildTrendCompressedInput(symbol, interval, candles, opts)
	if err != nil {
		return "", err
	}
	var out []byte
	if opts.Pretty {
		out, err = json.MarshalIndent(payload, "", "  ")
	} else {
		out, err = json.Marshal(payload)
	}
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func BuildTrendCompressedInput(symbol, interval string, candles []market.Candle, opts TrendCompressOptions) (TrendCompressedInput, error) {
	if len(candles) == 0 {
		return TrendCompressedInput{}, fmt.Errorf("no candles")
	}
	opts = normalizeTrendCompressOptions(opts)
	n := len(candles)

	closes := make([]float64, n)
	highs := make([]float64, n)
	lows := make([]float64, n)
	volumes := make([]float64, n)
	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
		volumes[i] = c.Volume
	}

	rsiSeries := talib.Rsi(closes, opts.RSIPeriod)
	atrSeries := talib.Atr(highs, lows, closes, opts.ATRPeriod)

	meta := TrendCompressedMeta{
		Symbol:    strings.TrimSpace(symbol),
		Interval:  strings.TrimSpace(interval),
		Timestamp: candleTimestamp(candles[n-1]),
	}

	gc := TrendGlobalContext{
		TrendSlope: roundFloat(linRegSlope(closes), 4),
		VolRatio:   roundFloat(volumeRatio(volumes, opts.VolumeMAPeriod), 3),
		Window:     n,
	}
	gc.NormalizedSlope = roundFloat(normalizedSlope(closes), 4)
	gc.SlopeState = trendSlopeState(gc.NormalizedSlope)
	if v := lastNonZero(talib.Ema(closes, opts.EMA20Period)); v > 0 {
		v = roundFloat(v, 4)
		gc.EMA20 = &v
	}
	if v := lastNonZero(talib.Ema(closes, opts.EMA50Period)); v > 0 {
		v = roundFloat(v, 4)
		gc.EMA50 = &v
	}
	if v := lastNonZero(talib.Ema(closes, opts.EMA200Period)); v > 0 {
		v = roundFloat(v, 4)
		gc.EMA200 = &v
	}

	structurePoints := selectStructurePoints(candles, highs, lows, rsiSeries, atrSeries, opts)
	candidates := buildStructureCandidates(candles, highs, lows, atrSeries, gc, structurePoints, opts)
	recentCandles := buildRecentCandles(candles, rsiSeries, opts)

	return TrendCompressedInput{
		Meta:                meta,
		StructurePoints:     structurePoints,
		StructureCandidates: candidates,
		RecentCandles:       recentCandles,
		GlobalContext:       gc,
	}, nil
}

func normalizeTrendCompressOptions(opts TrendCompressOptions) TrendCompressOptions {
	def := DefaultTrendCompressOptions()
	if opts.FractalSpan <= 0 {
		opts.FractalSpan = def.FractalSpan
	}
	if opts.MaxStructurePoints <= 0 {
		opts.MaxStructurePoints = def.MaxStructurePoints
	}
	if opts.DedupDistanceBars <= 0 {
		opts.DedupDistanceBars = def.DedupDistanceBars
	}
	if opts.DedupATRFactor <= 0 {
		opts.DedupATRFactor = def.DedupATRFactor
	}
	if opts.RSIPeriod <= 0 {
		opts.RSIPeriod = def.RSIPeriod
	}
	if opts.ATRPeriod <= 0 {
		opts.ATRPeriod = def.ATRPeriod
	}
	if opts.RecentCandles <= 0 {
		opts.RecentCandles = def.RecentCandles
	}
	if opts.VolumeMAPeriod <= 0 {
		opts.VolumeMAPeriod = def.VolumeMAPeriod
	}
	if opts.EMA20Period <= 0 {
		opts.EMA20Period = def.EMA20Period
	}
	if opts.EMA50Period <= 0 {
		opts.EMA50Period = def.EMA50Period
	}
	if opts.EMA200Period <= 0 {
		opts.EMA200Period = def.EMA200Period
	}
	if !opts.IncludeCurrentRSI && !opts.IncludeStructureRSI {
		opts.IncludeCurrentRSI = def.IncludeCurrentRSI
		opts.IncludeStructureRSI = def.IncludeStructureRSI
	}
	return opts
}

func buildRecentCandles(candles []market.Candle, rsi []float64, opts TrendCompressOptions) []TrendRecentCandle {
	n := len(candles)
	keep := opts.RecentCandles
	if keep > n {
		keep = n
	}
	start := n - keep
	out := make([]TrendRecentCandle, 0, keep)
	for idx := start; idx < n; idx++ {
		c := candles[idx]
		rc := TrendRecentCandle{
			Idx: idx,
			O:   roundFloat(c.Open, 4),
			H:   roundFloat(c.High, 4),
			L:   roundFloat(c.Low, 4),
			C:   roundFloat(c.Close, 4),
			V:   roundFloat(c.Volume, 4),
		}
		if opts.IncludeCurrentRSI && idx == n-1 && idx < len(rsi) {
			v := roundFloat(rsi[idx], 1)
			rc.RSI = &v
		}
		out = append(out, rc)
	}
	return out
}

func selectStructurePoints(candles []market.Candle, highs, lows, rsi, atr []float64, opts TrendCompressOptions) []TrendStructurePoint {
	n := len(candles)
	span := opts.FractalSpan
	if n < span*2+1 {
		return nil
	}
	selected := make([]TrendStructurePoint, 0, opts.MaxStructurePoints)
	for idx := n - span - 1; idx >= span; idx-- {
		if isFractalHigh(highs, idx, span) {
			p := TrendStructurePoint{Idx: idx, Type: "High", Price: roundFloat(highs[idx], 4)}
			if opts.IncludeStructureRSI && idx < len(rsi) {
				v := roundFloat(rsi[idx], 1)
				p.RSI = &v
			}
			selected = mergeStructurePoint(selected, p, atr, opts)
		}
		if isFractalLow(lows, idx, span) {
			p := TrendStructurePoint{Idx: idx, Type: "Low", Price: roundFloat(lows[idx], 4)}
			if opts.IncludeStructureRSI && idx < len(rsi) {
				v := roundFloat(rsi[idx], 1)
				p.RSI = &v
			}
			selected = mergeStructurePoint(selected, p, atr, opts)
		}
		if len(selected) >= opts.MaxStructurePoints {
			continue
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Idx < selected[j].Idx })
	return selected
}

func mergeStructurePoint(existing []TrendStructurePoint, candidate TrendStructurePoint, atr []float64, opts TrendCompressOptions) []TrendStructurePoint {
	for i := range existing {
		other := existing[i]
		if other.Type != candidate.Type {
			continue
		}
		distance := absInt(other.Idx - candidate.Idx)
		if distance >= opts.DedupDistanceBars {
			continue
		}
		threshold := 0.0
		if candidate.Idx >= 0 && candidate.Idx < len(atr) {
			threshold = atr[candidate.Idx] * opts.DedupATRFactor
		}
		if threshold <= 0 && other.Idx >= 0 && other.Idx < len(atr) {
			threshold = atr[other.Idx] * opts.DedupATRFactor
		}
		if threshold <= 0 {
			continue
		}
		if math.Abs(other.Price-candidate.Price) >= threshold {
			continue
		}
		switch candidate.Type {
		case "High":
			if candidate.Price > other.Price {
				existing[i] = candidate
			}
		case "Low":
			if candidate.Price < other.Price {
				existing[i] = candidate
			}
		}
		return existing
	}
	if len(existing) >= opts.MaxStructurePoints {
		return existing
	}
	return append(existing, candidate)
}

func isFractalHigh(highs []float64, idx, span int) bool {
	v := highs[idx]
	for i := 1; i <= span; i++ {
		if v <= highs[idx-i] || v <= highs[idx+i] {
			return false
		}
	}
	return true
}

func isFractalLow(lows []float64, idx, span int) bool {
	v := lows[idx]
	for i := 1; i <= span; i++ {
		if v >= lows[idx-i] || v >= lows[idx+i] {
			return false
		}
	}
	return true
}

func linRegSlope(series []float64) float64 {
	n := len(series)
	if n == 0 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	fn := float64(n)
	for i, y := range series {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	denom := fn*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (fn*sumXY - sumX*sumY) / denom
}

func volumeRatio(volumes []float64, lookback int) float64 {
	n := len(volumes)
	if n == 0 {
		return 0
	}
	if lookback <= 0 {
		lookback = 20
	}
	last := volumes[n-1]
	if n < 2 {
		return 0
	}
	count := lookback
	if count > n-1 {
		count = n - 1
	}
	if count <= 0 {
		return 0
	}
	sum := 0.0
	for i := n - 1 - count; i < n-1; i++ {
		sum += volumes[i]
	}
	avg := sum / float64(count)
	if avg == 0 {
		return 0
	}
	return last / avg
}

func lastNonZero(series []float64) float64 {
	for i := len(series) - 1; i >= 0; i-- {
		v := series[i]
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if math.Abs(v) <= 1e-12 {
			continue
		}
		return v
	}
	return 0
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxFloat(values []float64) float64 {
	m := -math.MaxFloat64
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if v > m {
			m = v
		}
	}
	if m == -math.MaxFloat64 {
		return 0
	}
	return m
}

func minFloat(values []float64) float64 {
	m := math.MaxFloat64
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if v < m {
			m = v
		}
	}
	if m == math.MaxFloat64 {
		return 0
	}
	return m
}

func normalizedSlope(series []float64) float64 {
	if len(series) < 2 {
		return 0
	}
	first := series[0]
	last := series[len(series)-1]
	if math.Abs(first) < 1e-9 {
		return 0
	}
	return (last - first) / math.Abs(first) * 100 / float64(len(series)-1)
}

func trendSlopeState(norm float64) string {
	abs := math.Abs(norm)
	switch {
	case abs < 0.1:
		return "FLAT"
	case abs < 0.4:
		return "MODERATE"
	default:
		return "STEEP"
	}
}

func buildStructureCandidates(candles []market.Candle, highs, lows, atr []float64, gc TrendGlobalContext, points []TrendStructurePoint, opts TrendCompressOptions) []TrendStructureCandidate {
	n := len(candles)
	if n == 0 {
		return nil
	}
	cands := make([]TrendStructureCandidate, 0, 12)
	atrLatest := 0.0
	if len(atr) > 0 {
		atrLatest = atr[len(atr)-1]
	}

	// Fractal points作为候选
	for _, p := range points {
		age := n - 1 - p.Idx
		source := "fractal_low"
		typ := "support"
		if strings.EqualFold(p.Type, "High") {
			source = "fractal_high"
			typ = "resistance"
		}
		cands = append(cands, TrendStructureCandidate{
			Price:      p.Price,
			Type:       typ,
			Source:     source,
			AgeCandles: age,
		})
	}

	// EMA 作为动态支撑/阻力参考
	addEMA := func(val *float64, source string, window int) {
		if val == nil {
			return
		}
		cands = append(cands, TrendStructureCandidate{
			Price:  roundFloat(*val, 4),
			Type:   "ema",
			Source: source,
			Window: window,
		})
	}
	addEMA(gc.EMA20, "ema20", opts.EMA20Period)
	addEMA(gc.EMA50, "ema50", opts.EMA50Period)
	addEMA(gc.EMA200, "ema200", opts.EMA200Period)

	// 布林带上下轨
	if n >= opts.VolumeMAPeriod {
		upper, _, lower := talib.BBands(extractCloses(candles), opts.VolumeMAPeriod, 2, 2, talib.SMA)
		if u := lastNonZero(upper); u > 0 {
			cands = append(cands, TrendStructureCandidate{
				Price:  roundFloat(u, 4),
				Type:   "band_upper",
				Source: "bollinger_upper",
				Window: opts.VolumeMAPeriod,
			})
		}
		if l := lastNonZero(lower); l > 0 {
			cands = append(cands, TrendStructureCandidate{
				Price:  roundFloat(l, 4),
				Type:   "band_lower",
				Source: "bollinger_lower",
				Window: opts.VolumeMAPeriod,
			})
		}
	}

	// 近期区间高低
	rangeWin := 30
	if rangeWin > n {
		rangeWin = n
	}
	if rangeWin > 0 {
		hi := maxFloat(highs[n-rangeWin:])
		lo := minFloat(lows[n-rangeWin:])
		cands = append(cands, TrendStructureCandidate{
			Price:  roundFloat(hi, 4),
			Type:   "range_high",
			Source: "range_high",
			Window: rangeWin,
		})
		cands = append(cands, TrendStructureCandidate{
			Price:  roundFloat(lo, 4),
			Type:   "range_low",
			Source: "range_low",
			Window: rangeWin,
		})
	}

	return dedupCandidates(cands, atrLatest, opts)
}

func extractCloses(candles []market.Candle) []float64 {
	out := make([]float64, 0, len(candles))
	for _, c := range candles {
		out = append(out, c.Close)
	}
	return out
}

func dedupCandidates(in []TrendStructureCandidate, atr float64, opts TrendCompressOptions) []TrendStructureCandidate {
	if len(in) == 0 {
		return nil
	}
	threshold := atr * opts.DedupATRFactor
	if threshold <= 0 {
		threshold = 0
	}
	out := make([]TrendStructureCandidate, 0, len(in))
	for _, c := range in {
		merged := false
		for i := range out {
			if out[i].Type != c.Type {
				continue
			}
			if threshold > 0 && math.Abs(out[i].Price-c.Price) <= threshold {
				// 保留较新或来源更明确的
				if c.AgeCandles < out[i].AgeCandles || out[i].AgeCandles == 0 {
					out[i] = c
				}
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AgeCandles != out[j].AgeCandles {
			return out[i].AgeCandles < out[j].AgeCandles
		}
		return out[i].Price < out[j].Price
	})
	return out
}
