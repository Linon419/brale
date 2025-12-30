package decision

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"brale/internal/analysis/indicator"
	"brale/internal/market"

	talib "github.com/markcheno/go-talib"
)

const (
	indicatorSnapshotVersion   = "indicator_snapshot_v1"
	defaultDivergenceLookback  = 6
	multiDivPivotPeriod        = 5
	multiDivMaxPivotPoints     = 10
	multiDivMaxBars            = 100
	multiDivMinCount           = 1
	multiDivDontConfirm        = false
	multiDivSearchMode         = "regular_hidden"
	multiDivSource             = "close"
)

type indicatorSnapshot struct {
	Meta   snapshotMeta   `json:"_meta"`
	Market snapshotMarket `json:"market"`
	Data   snapshotData   `json:"data"`
}

type snapshotMeta struct {
	SeriesOrder  string           `json:"series_order"`
	SampledAt    string           `json:"sampled_at"`
	Version      string           `json:"version"`
	TimestampNow string           `json:"timestamp_now_ts,omitempty"`
	DataAgeSec   map[string]int64 `json:"data_age_sec,omitempty"`
}

type snapshotMarket struct {
	Symbol         string  `json:"symbol"`
	Interval       string  `json:"interval"`
	CurrentPrice   float64 `json:"current_price"`
	PriceTimestamp string  `json:"price_timestamp"`
}

type snapshotData struct {
	EMAFast *emaSnapshot   `json:"ema_fast,omitempty"`
	EMAMid  *emaSnapshot   `json:"ema_mid,omitempty"`
	EMASlow *emaSnapshot   `json:"ema_slow,omitempty"`
	EMALong *emaSnapshot   `json:"ema_long,omitempty"`
	MACD    *macdSnapshot  `json:"macd,omitempty"`
	RSI     *rsiSnapshot   `json:"rsi,omitempty"`
	OBV     *obvSnapshot   `json:"obv,omitempty"`
	StochK  *stochSnapshot `json:"stoch_k,omitempty"`
	ATR     *atrSnapshot   `json:"atr,omitempty"`
	WTMFI   *wtmfiSnapshot `json:"wt_mfi_hybrid,omitempty"`
	DivMulti *multiDivSnapshot `json:"divergence_multi,omitempty"`
}

type emaSnapshot struct {
	Latest       float64   `json:"latest"`
	LastN        []float64 `json:"last_n,omitempty"`
	PeriodHigh   float64   `json:"period_high"`
	PeriodLow    float64   `json:"period_low"`
	DeltaToPrice float64   `json:"delta_to_price"`
	DeltaPct     float64   `json:"delta_pct"`
}

type macdSnapshot struct {
	DIF             float64         `json:"dif"`
	DEA             float64         `json:"dea"`
	Histogram       *seriesSnapshot `json:"histogram,omitempty"`
	Slope           *float64        `json:"slope,omitempty"`
	NormalizedSlope *float64        `json:"normalized_slope,omitempty"`
	SlopeState      string          `json:"slope_state,omitempty"`
	Divergence      string          `json:"divergence,omitempty"`
}

type rsiSnapshot struct {
	Current         float64   `json:"current"`
	LastN           []float64 `json:"last_n,omitempty"`
	PeriodHigh      float64   `json:"period_high"`
	PeriodLow       float64   `json:"period_low"`
	DistanceToHigh  float64   `json:"distance_to_high"`
	DistanceToLow   float64   `json:"distance_to_low"`
	Slope           *float64  `json:"slope,omitempty"`
	NormalizedSlope *float64  `json:"normalized_slope,omitempty"`
	SlopeState      string    `json:"slope_state,omitempty"`
}

type obvSnapshot struct {
	Latest float64   `json:"latest"`
	LastN  []float64 `json:"last_n,omitempty"`
}

type stochSnapshot struct {
	Current float64   `json:"current"`
	LastN   []float64 `json:"last_n,omitempty"`
	RangeLo float64   `json:"range_min"`
	RangeHi float64   `json:"range_max"`
}

type seriesSnapshot struct {
	Last []float64 `json:"last_n,omitempty"`
}

type atrSnapshot struct {
	Latest    float64   `json:"latest"`
	LastN     []float64 `json:"last_n,omitempty"`
	RangeLo   float64   `json:"range_min"`
	RangeHi   float64   `json:"range_max"`
	ChangePct *float64  `json:"change_pct,omitempty"`
}

type wtmfiSnapshot struct {
	Latest          float64   `json:"latest"`
	LastN           []float64 `json:"last_n,omitempty"`
	PeriodHigh      float64   `json:"period_high"`
	PeriodLow       float64   `json:"period_low"`
	Overbought      float64   `json:"overbought,omitempty"`
	Oversold        float64   `json:"oversold,omitempty"`
	State           string    `json:"state,omitempty"`
	Slope           *float64  `json:"slope,omitempty"`
	NormalizedSlope *float64  `json:"normalized_slope,omitempty"`
	SlopeState      string    `json:"slope_state,omitempty"`
}

type multiDivSnapshot struct {
	Total                   int              `json:"total"`
	MinCount                int              `json:"min_count"`
	Source                  string           `json:"source"`
	SearchMode              string           `json:"search_mode"`
	PivotPeriod             int              `json:"pivot_period"`
	MaxPivotPoints          int              `json:"max_pivot_points"`
	MaxBars                 int              `json:"max_bars"`
	DontConfirm             bool             `json:"dont_confirm"`
	IndicatorNameMode       string           `json:"indicator_name_mode"`
	ShowNumbers             bool             `json:"show_numbers"`
	PositiveCount           int              `json:"positive_count"`
	NegativeCount           int              `json:"negative_count"`
	LabelTop                string           `json:"label_top,omitempty"`
	LabelBottom             string           `json:"label_bottom,omitempty"`
	PositiveRegularDetected bool             `json:"positive_regular_detected"`
	NegativeRegularDetected bool             `json:"negative_regular_detected"`
	PositiveHiddenDetected  bool             `json:"positive_hidden_detected"`
	NegativeHiddenDetected  bool             `json:"negative_hidden_detected"`
	PositiveDetected        bool             `json:"positive_detected"`
	NegativeDetected        bool             `json:"negative_detected"`
	Signals                 []multiDivSignal `json:"signals,omitempty"`
	// Scoring system fields
	Direction     string  `json:"direction,omitempty"`      // up, down, conflict, none
	BullishScore  float64 `json:"bullish_score,omitempty"`
	BearishScore  float64 `json:"bearish_score,omitempty"`
	BullishThresh float64 `json:"bullish_threshold,omitempty"`
	BearishThresh float64 `json:"bearish_threshold,omitempty"`
}

type multiDivSignal struct {
	Indicator string `json:"indicator"`
	Type      string `json:"type"`
	Distance  int    `json:"distance"`
}

func BuildIndicatorSnapshot(candles []market.Candle, rep indicator.Report, wtmfiSettings indicator.WTMFISettings, disableRSI bool) ([]byte, error) {
	if len(candles) == 0 {
		return nil, fmt.Errorf("indicator snapshot: no candles")
	}
	if len(rep.Values) == 0 {
		return nil, fmt.Errorf("indicator snapshot: empty report")
	}
	last := candles[len(candles)-1]
	stamp := candleTimestamp(last)
	price := last.Close
	now := time.Now().UTC()
	snapshot := indicatorSnapshot{
		Meta: snapshotMeta{
			SeriesOrder:  "oldest_to_latest",
			SampledAt:    stamp,
			Version:      indicatorSnapshotVersion,
			TimestampNow: now.Format(time.RFC3339),
		},
		Market: snapshotMarket{
			Symbol:         strings.ToUpper(strings.TrimSpace(rep.Symbol)),
			Interval:       strings.ToLower(strings.TrimSpace(rep.Interval)),
			CurrentPrice:   roundFloat(price, 4),
			PriceTimestamp: stamp,
		},
	}
	if last.CloseTime > 0 {
		ageSec := int64(now.Sub(time.UnixMilli(last.CloseTime)).Seconds())
		if ageSec < 0 {
			ageSec = 0
		}
		snapshot.Meta.DataAgeSec = map[string]int64{"indicator": ageSec}
	}
	data := snapshotData{}
	if val, ok := rep.Values["ema_fast"]; ok {
		data.EMAFast = buildEMASnapshot(val, price, 5)
	}
	if val, ok := rep.Values["ema_mid"]; ok {
		data.EMAMid = buildEMASnapshot(val, price, 4)
	}
	if val, ok := rep.Values["ema_slow"]; ok {
		data.EMASlow = buildEMASnapshot(val, price, 3)
	}
	if val, ok := rep.Values["ema_long"]; ok {
		data.EMALong = buildEMASnapshot(val, price, 3)
	}
	if _, ok := rep.Values["macd"]; ok {
		if snap := buildMACDSnapshot(candles, 3); snap != nil {
			data.MACD = snap
		}
	}
	if !disableRSI {
		if val, ok := rep.Values["rsi"]; ok {
			data.RSI = buildRSISnapshot(val)
		}
	}
	if val, ok := rep.Values["obv"]; ok {
		data.OBV = buildOBVSnapshot(val)
	}
	if val, ok := rep.Values["stoch_k"]; ok {
		data.StochK = buildStochSnapshot(val)
	}
	if val, ok := rep.Values["atr"]; ok {
		data.ATR = buildATRSnapshot(val)
	}
	if val, ok := rep.Values["wt_mfi_hybrid"]; ok {
		normalized := indicator.NormalizeWTMFISettings(wtmfiSettings)
		data.WTMFI = buildWTMFISnapshot(val, normalized.Overbought, normalized.Oversold)
	}
	if snap := buildMultiDivSnapshot(candles, !disableRSI); snap != nil {
		data.DivMulti = snap
	}
	snapshot.Data = data
	return json.Marshal(snapshot)
}

func buildEMASnapshot(val indicator.IndicatorValue, price float64, tail int) *emaSnapshot {
	if val.Latest == 0 && len(val.Series) == 0 {
		return nil
	}
	maxVal, minVal := seriesBounds(val.Series)
	delta := price - val.Latest
	deltaPct := 0.0
	if val.Latest != 0 {
		deltaPct = (delta / val.Latest) * 100
	}
	return &emaSnapshot{
		Latest:       roundFloat(val.Latest, 4),
		LastN:        roundSeriesTail(val.Series, tail),
		PeriodHigh:   roundFloat(maxVal, 4),
		PeriodLow:    roundFloat(minVal, 4),
		DeltaToPrice: roundFloat(delta, 4),
		DeltaPct:     roundFloat(deltaPct, 4),
	}
}

func buildMACDSnapshot(candles []market.Candle, tail int) *macdSnapshot {
	if len(candles) == 0 {
		return nil
	}
	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}
	macdSeries, signalSeries, histSeries := talib.Macd(closes, 12, 26, 9)
	mSeries := sanitizeSeries(macdSeries)
	sSeries := sanitizeSeries(signalSeries)
	hSeries := sanitizeSeries(histSeries)
	if len(mSeries) == 0 || len(sSeries) == 0 || len(hSeries) == 0 {
		return nil
	}
	histLast := roundSeriesTail(hSeries, tail)
	var hist *seriesSnapshot
	if len(histLast) > 0 {
		hist = &seriesSnapshot{Last: histLast}
	}
	ms := &macdSnapshot{
		DIF:       roundFloat(mSeries[len(mSeries)-1], 4),
		DEA:       roundFloat(sSeries[len(sSeries)-1], 4),
		Histogram: hist,
	}
	if slope, norm := computeSlope(histLast); slope != nil {
		ms.Slope = slope
		ms.NormalizedSlope = norm
		ms.SlopeState = indicatorSlopeState(norm)
	}
	ms.Divergence = computeDivergence(closes, hSeries, defaultDivergenceLookback)
	return ms
}

func buildRSISnapshot(val indicator.IndicatorValue) *rsiSnapshot {
	if val.Latest == 0 && len(val.Series) == 0 {
		return nil
	}
	maxVal, minVal := seriesBounds(val.Series)
	rs := &rsiSnapshot{
		Current:        roundFloat(val.Latest, 4),
		LastN:          roundSeriesTail(val.Series, 3),
		PeriodHigh:     roundFloat(maxVal, 4),
		PeriodLow:      roundFloat(minVal, 4),
		DistanceToHigh: roundFloat(maxVal-val.Latest, 4),
		DistanceToLow:  roundFloat(val.Latest-minVal, 4),
	}
	if slope, norm := computeSlope(rs.LastN); slope != nil {
		rs.Slope = slope
		rs.NormalizedSlope = norm
		rs.SlopeState = indicatorSlopeState(norm)
	}
	return rs
}

func buildOBVSnapshot(val indicator.IndicatorValue) *obvSnapshot {
	if len(val.Series) == 0 {
		return nil
	}
	return &obvSnapshot{
		Latest: roundFloat(val.Latest, 4),
		LastN:  roundSeriesTail(val.Series, 3),
	}
}

func buildStochSnapshot(val indicator.IndicatorValue) *stochSnapshot {
	if len(val.Series) == 0 {
		return nil
	}
	return &stochSnapshot{
		Current: roundFloat(val.Latest, 4),
		LastN:   roundSeriesTail(val.Series, 2),
		RangeLo: 0,
		RangeHi: 100,
	}
}

func buildATRSnapshot(val indicator.IndicatorValue) *atrSnapshot {
	if val.Latest == 0 && len(val.Series) == 0 {
		return nil
	}
	maxVal, minVal := seriesBounds(val.Series)
	as := &atrSnapshot{
		Latest:  roundFloat(val.Latest, 4),
		LastN:   roundSeriesTail(val.Series, 3),
		RangeLo: roundFloat(minVal, 4),
		RangeHi: roundFloat(maxVal, 4),
	}
	if change := computeChangePct(val.Series); change != nil {
		as.ChangePct = change
	}
	return as
}

func buildWTMFISnapshot(val indicator.IndicatorValue, overbought, oversold float64) *wtmfiSnapshot {
	if val.Latest == 0 && len(val.Series) == 0 {
		return nil
	}
	maxVal, minVal := seriesBounds(val.Series)
	ws := &wtmfiSnapshot{
		Latest:     roundFloat(val.Latest, 4),
		LastN:      roundSeriesTail(val.Series, 3),
		PeriodHigh: roundFloat(maxVal, 4),
		PeriodLow:  roundFloat(minVal, 4),
		Overbought: overbought,
		Oversold:   oversold,
		State:      val.State,
	}
	if slope, norm := computeSlope(ws.LastN); slope != nil {
		ws.Slope = slope
		ws.NormalizedSlope = norm
		ws.SlopeState = indicatorSlopeState(norm)
	}
	return ws
}

// Multi-indicator divergence snapshot (regular + hidden).
func buildMultiDivSnapshot(candles []market.Candle, useRSI bool) *multiDivSnapshot {
	if len(candles) < multiDivPivotPeriod*2+2 {
		return nil
	}
	closes, highs, lows, volumes := extractSeries(candles)
	if len(closes) == 0 {
		return nil
	}
	pivotHighSrc := closes
	pivotLowSrc := closes
	if multiDivSource == "high_low" {
		pivotHighSrc = highs
		pivotLowSrc = lows
	}
	phPos, phVals := collectPivots(pivotHighSrc, multiDivPivotPeriod, true, multiDivMaxPivotPoints)
	plPos, plVals := collectPivots(pivotLowSrc, multiDivPivotPeriod, false, multiDivMaxPivotPoints)
	if len(phPos) == 0 && len(plPos) == 0 {
		return nil
	}

	prscLow := closes
	prscHigh := closes
	if multiDivSource == "high_low" {
		prscLow = lows
		prscHigh = highs
	}

	macd, _, macdHist := talib.Macd(closes, 12, 26, 9)
	var rsi []float64
	if useRSI {
		rsi = talib.Rsi(closes, 14)
	}
	stoch := smaSeries(stochFastK(closes, highs, lows, 14), 3)
	cci := talib.Cci(highs, lows, closes, 10)
	mom := talib.Mom(closes, 10)
	obv := talib.Obv(closes, volumes)
	vwmacd := diffSeries(vwmaSeries(closes, volumes, 12), vwmaSeries(closes, volumes, 26))
	cmf := cmfSeries(highs, lows, closes, volumes, 21)
	mfi := talib.Mfi(highs, lows, closes, volumes, 14)

	seriesList := make([]struct {
		name   string
		label  string
		series []float64
	}, 0, 10)
	seriesList = append(seriesList,
		struct {
			name   string
			label  string
			series []float64
		}{name: "macd", label: "MACD", series: macd},
		struct {
			name   string
			label  string
			series []float64
		}{name: "macd_hist", label: "Hist", series: macdHist},
	)
	if useRSI {
		seriesList = append(seriesList, struct {
			name   string
			label  string
			series []float64
		}{name: "rsi", label: "RSI", series: rsi})
	}
	seriesList = append(seriesList,
		struct {
			name   string
			label  string
			series []float64
		}{name: "stoch", label: "Stoch", series: stoch},
		struct {
			name   string
			label  string
			series []float64
		}{name: "cci", label: "CCI", series: cci},
		struct {
			name   string
			label  string
			series []float64
		}{name: "mom", label: "MOM", series: mom},
		struct {
			name   string
			label  string
			series []float64
		}{name: "obv", label: "OBV", series: obv},
		struct {
			name   string
			label  string
			series []float64
		}{name: "vwmacd", label: "VWMACD", series: vwmacd},
		struct {
			name   string
			label  string
			series []float64
		}{name: "cmf", label: "CMF", series: cmf},
		struct {
			name   string
			label  string
			series []float64
		}{name: "mfi", label: "MFI", series: mfi},
	)

	allowRegular := multiDivSearchMode == "regular" || multiDivSearchMode == "regular_hidden"
	allowHidden := multiDivSearchMode == "hidden" || multiDivSearchMode == "regular_hidden"

	signals := make([]multiDivSignal, 0)
	total := 0
	posCount := 0
	negCount := 0
	posRegDetected := false
	negRegDetected := false
	posHidDetected := false
	negHidDetected := false
	labelTop := ""
	labelBottom := ""
	for _, item := range seriesList {
		if len(item.series) == 0 {
			continue
		}
		divType := -1
		divs := [4]int{}
		if allowRegular {
			divs[0] = positiveRegularPositiveHidden(item.series, closes, prscLow, plPos, plVals, 1)
			divs[1] = negativeRegularNegativeHidden(item.series, closes, prscHigh, phPos, phVals, 1)
		}
		if allowHidden {
			divs[2] = positiveRegularPositiveHidden(item.series, closes, prscLow, plPos, plVals, 2)
			divs[3] = negativeRegularNegativeHidden(item.series, closes, prscHigh, phPos, phVals, 2)
		}
		for y := 0; y < 4; y++ {
			if divs[y] <= 0 {
				continue
			}
			divType = y
			total++
			if y%2 == 1 {
				negCount++
			} else {
				posCount++
			}
			switch y {
			case 0:
				posRegDetected = true
			case 1:
				negRegDetected = true
			case 2:
				posHidDetected = true
			case 3:
				negHidDetected = true
			}
			signals = append(signals, multiDivSignal{
				Indicator: item.name,
				Type:      divergenceTypeLabel(y),
				Distance:  divs[y],
			})
		}
		if divType >= 0 {
			if divType%2 == 1 {
				labelTop += item.label + "\n"
			} else {
				labelBottom += item.label + "\n"
			}
		}
	}

	if total < multiDivMinCount {
		total = 0
		posCount = 0
		negCount = 0
		posRegDetected = false
		negRegDetected = false
		posHidDetected = false
		negHidDetected = false
		labelTop = ""
		labelBottom = ""
		signals = nil
	}

	if posCount > 0 {
		labelBottom += fmt.Sprintf("%d", posCount)
	}
	if negCount > 0 {
		labelTop += fmt.Sprintf("%d", negCount)
	}

	// Calculate scores using default scorer
	scoreResult := calcDivScore(signals)

	return &multiDivSnapshot{
		Total:                   total,
		MinCount:                multiDivMinCount,
		Source:                  multiDivSource,
		SearchMode:              multiDivSearchMode,
		PivotPeriod:             multiDivPivotPeriod,
		MaxPivotPoints:          multiDivMaxPivotPoints,
		MaxBars:                 multiDivMaxBars,
		DontConfirm:             multiDivDontConfirm,
		IndicatorNameMode:       "full",
		ShowNumbers:             true,
		PositiveCount:           posCount,
		NegativeCount:           negCount,
		LabelTop:                labelTop,
		LabelBottom:             labelBottom,
		PositiveRegularDetected: posRegDetected,
		NegativeRegularDetected: negRegDetected,
		PositiveHiddenDetected:  posHidDetected,
		NegativeHiddenDetected:  negHidDetected,
		PositiveDetected:        posRegDetected || posHidDetected,
		NegativeDetected:        negRegDetected || negHidDetected,
		Signals:                 signals,
		Direction:               scoreResult.Direction,
		BullishScore:            scoreResult.BullishScore,
		BearishScore:            scoreResult.BearishScore,
		BullishThresh:           scoreResult.BullishThresh,
		BearishThresh:           scoreResult.BearishThresh,
	}
}

// calcDivScore calculates divergence score with default weights
func calcDivScore(signals []multiDivSignal) DivScoreResult {
	if len(signals) == 0 {
		return DivScoreResult{Direction: "none"}
	}

	var bullishScore, bearishScore float64
	var bullishMax, bearishMax float64

	for _, sig := range signals {
		weight := getDefaultWeight(sig.Indicator)
		isBullish := sig.Type == "positive_regular" || sig.Type == "positive_hidden"
		if isBullish {
			bullishScore += weight
			bullishMax += weight
		} else {
			bearishScore += weight
			bearishMax += weight
		}
	}

	bullishThresh := bullishMax * thresholdRatio
	bearishThresh := bearishMax * thresholdRatio

	bullishValid := bullishScore >= bullishThresh && bullishThresh > 0
	bearishValid := bearishScore >= bearishThresh && bearishThresh > 0

	direction := "none"
	switch {
	case bullishValid && bearishValid:
		direction = "conflict"
	case bullishValid:
		direction = "up"
	case bearishValid:
		direction = "down"
	}

	return DivScoreResult{
		Direction:     direction,
		BullishScore:  roundFloat(bullishScore, 2),
		BearishScore:  roundFloat(bearishScore, 2),
		BullishThresh: roundFloat(bullishThresh, 2),
		BearishThresh: roundFloat(bearishThresh, 2),
	}
}

func getDefaultWeight(indicator string) float64 {
	if momentumIndicators[indicator] {
		return baseMomentumWeight
	}
	if volumeIndicators[indicator] {
		return baseVolumeWeight
	}
	return 1.0
}

func roundSeriesTail(series []float64, n int) []float64 {
	if n <= 0 || len(series) == 0 {
		return nil
	}
	start := len(series) - n
	if start < 0 {
		start = 0
	}
	out := make([]float64, 0, len(series)-start)
	for i := start; i < len(series); i++ {
		out = append(out, roundFloat(series[i], 4))
	}
	return out
}

func seriesBounds(series []float64) (max, min float64) {
	if len(series) == 0 {
		return 0, 0
	}
	max = -math.MaxFloat64
	min = math.MaxFloat64
	for _, v := range series {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}
	if max == -math.MaxFloat64 {
		max = 0
	}
	if min == math.MaxFloat64 {
		min = 0
	}
	return roundFloat(max, 4), roundFloat(min, 4)
}

func roundFloat(v float64, digits int) float64 {
	if digits <= 0 {
		return math.Round(v)
	}
	factor := math.Pow10(digits)
	return math.Round(v*factor) / factor
}

func sanitizeSeries(series []float64) []float64 {
	if len(series) == 0 {
		return nil
	}
	out := make([]float64, 0, len(series))
	for _, v := range series {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out = append(out, roundFloat(v, 4))
	}
	return out
}

func candleTimestamp(c market.Candle) string {
	ts := c.CloseTime
	if ts == 0 {
		ts = c.OpenTime
	}
	if ts == 0 {
		return time.Now().UTC().Format(time.RFC3339)
	}
	return time.UnixMilli(ts).UTC().Format(time.RFC3339)
}

func computeSlope(series []float64) (*float64, *float64) {
	if len(series) < 2 {
		return nil, nil
	}
	start := 0
	if len(series) > 5 {
		start = len(series) - 5
	}
	first := series[start]
	last := series[len(series)-1]
	steps := float64(len(series) - start - 1)
	if steps <= 0 {
		return nil, nil
	}
	delta := last - first
	raw := roundFloat(delta/steps, 4)
	var norm *float64
	if math.Abs(first) > 1e-9 {
		v := roundFloat((delta/math.Abs(first))*100/steps, 4)
		norm = &v
	}
	return &raw, norm
}

func computeChangePct(series []float64) *float64 {
	if len(series) < 2 {
		return nil
	}
	last := series[len(series)-1]
	prev := series[len(series)-2]
	if math.Abs(prev) <= 1e-9 {
		return nil
	}
	v := roundFloat(((last-prev)/prev)*100, 4)
	return &v
}

func indicatorSlopeState(norm *float64) string {
	if norm == nil {
		return ""
	}
	abs := math.Abs(*norm)
	switch {
	case abs < 0.1:
		return "FLAT"
	case abs < 0.4:
		return "MODERATE"
	default:
		return "STEEP"
	}
}

func computeDivergence(prices, indicators []float64, lookback int) string {
	prices, indicators = alignSeries(prices, indicators)
	if lookback <= 0 || len(prices) <= lookback || len(indicators) <= lookback {
		return "neutral"
	}
	end := len(prices) - 1
	prev := end - lookback
	priceNow := prices[end]
	pricePrev := prices[prev]
	indNow := indicators[end]
	indPrev := indicators[prev]
	switch {
	case priceNow > pricePrev && indNow < indPrev:
		return "down"
	case priceNow < pricePrev && indNow > indPrev:
		return "up"
	default:
		return "neutral"
	}
}

func alignSeries(prices, indicators []float64) ([]float64, []float64) {
	if len(prices) == 0 || len(indicators) == 0 {
		return nil, nil
	}
	if len(prices) > len(indicators) {
		prices = prices[len(prices)-len(indicators):]
	} else if len(indicators) > len(prices) {
		indicators = indicators[len(indicators)-len(prices):]
	}
	return prices, indicators
}

func extractSeries(candles []market.Candle) (closes, highs, lows, volumes []float64) {
	n := len(candles)
	if n == 0 {
		return nil, nil, nil, nil
	}
	closes = make([]float64, n)
	highs = make([]float64, n)
	lows = make([]float64, n)
	volumes = make([]float64, n)
	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
		volumes[i] = c.Volume
	}
	return closes, highs, lows, volumes
}

func collectPivots(values []float64, prd int, isHigh bool, maxKeep int) ([]int, []float64) {
	if len(values) < prd*2+1 || prd <= 0 || maxKeep <= 0 {
		return nil, nil
	}
	positions := make([]int, 0, maxKeep)
	vals := make([]float64, 0, maxKeep)
	for i := len(values) - 1 - prd; i >= prd; i-- {
		if !isPivot(values, i, prd, isHigh) {
			continue
		}
		positions = append(positions, i)
		vals = append(vals, values[i])
		if len(positions) >= maxKeep {
			break
		}
	}
	return positions, vals
}

func isPivot(values []float64, idx, prd int, isHigh bool) bool {
	if idx-prd < 0 || idx+prd >= len(values) {
		return false
	}
	center := values[idx]
	if !isFinite(center) {
		return false
	}
	for i := idx - prd; i <= idx+prd; i++ {
		if i == idx {
			continue
		}
		v := values[i]
		if !isFinite(v) {
			return false
		}
		if isHigh && v > center {
			return false
		}
		if !isHigh && v < center {
			return false
		}
	}
	return true
}

func positiveRegularPositiveHidden(src, close, prsc []float64, pivPos []int, pivVals []float64, cond int) int {
	if len(src) == 0 || len(close) == 0 || len(prsc) == 0 || len(pivPos) == 0 {
		return 0
	}
	if !multiDivDontConfirm {
		src0, ok0 := seriesAt(src, 0)
		src1, ok1 := seriesAt(src, 1)
		close0, ok2 := seriesAt(close, 0)
		close1, ok3 := seriesAt(close, 1)
		if !(ok0 && ok1 && ok2 && ok3) || !(src0 > src1 || close0 > close1) {
			return 0
		}
	}

	startpoint := 0
	if !multiDivDontConfirm {
		startpoint = 1
	}
	lastIdx := len(close) - 1
	for x := 0; x < len(pivPos) && x < multiDivMaxPivotPoints; x++ {
		if pivPos[x] <= 0 {
			break
		}
		divLen := lastIdx - pivPos[x]
		if divLen > multiDivMaxBars {
			break
		}
		if divLen <= 5 || divLen-startpoint <= 0 {
			continue
		}
		srcStart, ok1 := seriesAt(src, startpoint)
		srcLen, ok2 := seriesAt(src, divLen)
		prscStart, ok3 := seriesAt(prsc, startpoint)
		pivotVal := pivVals[x]
		if !(ok1 && ok2 && ok3 && isFinite(pivotVal)) {
			continue
		}
		okCond := false
		if cond == 1 {
			okCond = srcStart > srcLen && prscStart < pivotVal
		} else if cond == 2 {
			okCond = srcStart < srcLen && prscStart > pivotVal
		}
		if !okCond {
			continue
		}
		closeStart, ok4 := seriesAt(close, startpoint)
		closeLen, ok5 := seriesAt(close, divLen)
		if !(ok4 && ok5) {
			continue
		}
		slope1 := (srcStart - srcLen) / float64(divLen-startpoint)
		virtual1 := srcStart - slope1
		slope2 := (closeStart - closeLen) / float64(divLen-startpoint)
		virtual2 := closeStart - slope2
		arrived := true
		for y := startpoint + 1; y <= divLen-1; y++ {
			srcY, okY := seriesAt(src, y)
			closeY, okC := seriesAt(close, y)
			if !(okY && okC) || srcY < virtual1 || closeY < virtual2 {
				arrived = false
				break
			}
			virtual1 -= slope1
			virtual2 -= slope2
		}
		if arrived {
			return divLen
		}
	}
	return 0
}

func negativeRegularNegativeHidden(src, close, prsc []float64, pivPos []int, pivVals []float64, cond int) int {
	if len(src) == 0 || len(close) == 0 || len(prsc) == 0 || len(pivPos) == 0 {
		return 0
	}
	if !multiDivDontConfirm {
		src0, ok0 := seriesAt(src, 0)
		src1, ok1 := seriesAt(src, 1)
		close0, ok2 := seriesAt(close, 0)
		close1, ok3 := seriesAt(close, 1)
		if !(ok0 && ok1 && ok2 && ok3) || !(src0 < src1 || close0 < close1) {
			return 0
		}
	}

	startpoint := 0
	if !multiDivDontConfirm {
		startpoint = 1
	}
	lastIdx := len(close) - 1
	for x := 0; x < len(pivPos) && x < multiDivMaxPivotPoints; x++ {
		if pivPos[x] <= 0 {
			break
		}
		divLen := lastIdx - pivPos[x]
		if divLen > multiDivMaxBars {
			break
		}
		if divLen <= 5 || divLen-startpoint <= 0 {
			continue
		}
		srcStart, ok1 := seriesAt(src, startpoint)
		srcLen, ok2 := seriesAt(src, divLen)
		prscStart, ok3 := seriesAt(prsc, startpoint)
		pivotVal := pivVals[x]
		if !(ok1 && ok2 && ok3 && isFinite(pivotVal)) {
			continue
		}
		okCond := false
		if cond == 1 {
			okCond = srcStart < srcLen && prscStart > pivotVal
		} else if cond == 2 {
			okCond = srcStart > srcLen && prscStart < pivotVal
		}
		if !okCond {
			continue
		}
		closeStart, ok4 := seriesAt(close, startpoint)
		closeLen, ok5 := seriesAt(close, divLen)
		if !(ok4 && ok5) {
			continue
		}
		slope1 := (srcStart - srcLen) / float64(divLen-startpoint)
		virtual1 := srcStart - slope1
		slope2 := (closeStart - closeLen) / float64(divLen-startpoint)
		virtual2 := closeStart - slope2
		arrived := true
		for y := startpoint + 1; y <= divLen-1; y++ {
			srcY, okY := seriesAt(src, y)
			closeY, okC := seriesAt(close, y)
			if !(okY && okC) || srcY > virtual1 || closeY > virtual2 {
				arrived = false
				break
			}
			virtual1 -= slope1
			virtual2 -= slope2
		}
		if arrived {
			return divLen
		}
	}
	return 0
}

func divergenceTypeLabel(idx int) string {
	switch idx {
	case 0:
		return "positive_regular"
	case 1:
		return "negative_regular"
	case 2:
		return "positive_hidden"
	case 3:
		return "negative_hidden"
	default:
		return "unknown"
	}
}

func seriesAt(series []float64, barsAgo int) (float64, bool) {
	if barsAgo < 0 || len(series) == 0 {
		return 0, false
	}
	idx := len(series) - 1 - barsAgo
	if idx < 0 || idx >= len(series) {
		return 0, false
	}
	val := series[idx]
	if !isFinite(val) {
		return 0, false
	}
	return val, true
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func stochFastK(closes, highs, lows []float64, period int) []float64 {
	out := make([]float64, len(closes))
	if period <= 1 || len(closes) == 0 {
		return out
	}
	for i := range closes {
		if i < period-1 {
			out[i] = math.NaN()
			continue
		}
		lo := lows[i]
		hi := highs[i]
		for j := i - period + 1; j <= i; j++ {
			if lows[j] < lo {
				lo = lows[j]
			}
			if highs[j] > hi {
				hi = highs[j]
			}
		}
		if hi-lo == 0 {
			out[i] = 0
			continue
		}
		out[i] = (closes[i]-lo)/(hi-lo)*100.0
	}
	return out
}

func smaSeries(series []float64, period int) []float64 {
	out := make([]float64, len(series))
	if period <= 1 || len(series) == 0 {
		copy(out, series)
		return out
	}
	for i := range series {
		if i < period-1 {
			out[i] = math.NaN()
			continue
		}
		sum := 0.0
		valid := true
		for j := i - period + 1; j <= i; j++ {
			if !isFinite(series[j]) {
				valid = false
				break
			}
			sum += series[j]
		}
		if !valid {
			out[i] = math.NaN()
			continue
		}
		out[i] = sum / float64(period)
	}
	return out
}

func vwmaSeries(closes, volumes []float64, period int) []float64 {
	out := make([]float64, len(closes))
	if period <= 1 || len(closes) == 0 {
		return out
	}
	for i := range closes {
		if i < period-1 {
			out[i] = math.NaN()
			continue
		}
		sumPV := 0.0
		sumV := 0.0
		valid := true
		for j := i - period + 1; j <= i; j++ {
			if !isFinite(closes[j]) || !isFinite(volumes[j]) {
				valid = false
				break
			}
			sumPV += closes[j] * volumes[j]
			sumV += volumes[j]
		}
		if !valid || sumV == 0 {
			out[i] = math.NaN()
			continue
		}
		out[i] = sumPV / sumV
	}
	return out
}

func diffSeries(a, b []float64) []float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return nil
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		if !isFinite(a[i]) || !isFinite(b[i]) {
			out[i] = math.NaN()
			continue
		}
		out[i] = a[i] - b[i]
	}
	return out
}

func cmfSeries(highs, lows, closes, volumes []float64, period int) []float64 {
	n := len(closes)
	out := make([]float64, n)
	if n == 0 || period <= 1 {
		return out
	}
	mfv := make([]float64, n)
	for i := range closes {
		hl := highs[i] - lows[i]
		if hl == 0 {
			mfv[i] = 0
			continue
		}
		cmfm := ((closes[i] - lows[i]) - (highs[i] - closes[i])) / hl
		mfv[i] = cmfm * volumes[i]
	}
	mfvSma := smaSeries(mfv, period)
	volSma := smaSeries(volumes, period)
	for i := range out {
		if !isFinite(mfvSma[i]) || !isFinite(volSma[i]) || volSma[i] == 0 {
			out[i] = math.NaN()
			continue
		}
		out[i] = mfvSma[i] / volSma[i]
	}
	return out
}
