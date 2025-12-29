package indicator

import (
	"fmt"
	"math"

	"github.com/markcheno/go-talib"

	"brale/internal/market"
)

type Settings struct {
	Symbol   string
	Interval string
	EMA      EMASettings
	RSI      RSISettings
	WTMFI    WTMFISettings
}

type EMASettings struct {
	Fast int `json:"fast,omitempty"`
	Mid  int `json:"mid,omitempty"`
	Slow int `json:"slow,omitempty"`
	Long int `json:"long,omitempty"`
}

type RSISettings struct {
	Period     int     `json:"period,omitempty"`
	Oversold   float64 `json:"oversold,omitempty"`
	Overbought float64 `json:"overbought,omitempty"`
}

type WTMFISettings struct {
	ChannelLen int     `json:"channel_len,omitempty"`
	AvgLen     int     `json:"avg_len,omitempty"`
	SmoothLen  int     `json:"smooth_len,omitempty"`
	MFILen     int     `json:"mfi_len,omitempty"`
	WTWeight   float64 `json:"wt_weight,omitempty"`
	MFIScale   float64 `json:"mfi_scale,omitempty"`
	Overbought float64 `json:"overbought,omitempty"`
	Oversold   float64 `json:"oversold,omitempty"`
}

type IndicatorValue struct {
	Latest float64   `json:"latest"`
	Series []float64 `json:"series,omitempty"`
	State  string    `json:"state,omitempty"`
	Note   string    `json:"note,omitempty"`
}

type Report struct {
	Symbol   string                    `json:"symbol"`
	Interval string                    `json:"interval"`
	Count    int                       `json:"count"`
	Values   map[string]IndicatorValue `json:"values"`
	Warnings []string                  `json:"warnings,omitempty"`
}

func ComputeAll(candles []market.Candle, cfg Settings) (Report, error) {
	rep := Report{
		Symbol:   cfg.Symbol,
		Interval: cfg.Interval,
		Count:    len(candles),
		Values:   make(map[string]IndicatorValue),
	}
	if len(candles) == 0 {
		return rep, fmt.Errorf("no candles")
	}
	closes := make([]float64, len(candles))
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	volumes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
		volumes[i] = c.Volume
	}

	if cfg.EMA.Fast <= 0 {
		cfg.EMA.Fast = 21
	}
	if cfg.EMA.Mid <= 0 {
		cfg.EMA.Mid = 55
	}
	if cfg.EMA.Slow <= 0 {
		cfg.EMA.Slow = 100
	}
	if cfg.EMA.Long <= 0 {
		cfg.EMA.Long = 200
	}
	emaFast := trimEMALeadingZeros(sanitizeSeries(talib.Ema(closes, cfg.EMA.Fast)))
	emaMid := trimEMALeadingZeros(sanitizeSeries(talib.Ema(closes, cfg.EMA.Mid)))
	emaSlow := trimEMALeadingZeros(sanitizeSeries(talib.Ema(closes, cfg.EMA.Slow)))
	emaLong := trimEMALeadingZeros(sanitizeSeries(talib.Ema(closes, cfg.EMA.Long)))
	lastClose := closes[len(closes)-1]
	rep.Values["ema_fast"] = IndicatorValue{
		Latest: lastValid(emaFast),
		Series: emaFast,
		State:  relativeState(lastClose, lastValid(emaFast)),
		Note:   fmt.Sprintf("EMA%d vs price", cfg.EMA.Fast),
	}
	rep.Values["ema_mid"] = IndicatorValue{
		Latest: lastValid(emaMid),
		Series: emaMid,
		State:  relativeState(lastClose, lastValid(emaMid)),
		Note:   fmt.Sprintf("EMA%d vs price", cfg.EMA.Mid),
	}
	rep.Values["ema_slow"] = IndicatorValue{
		Latest: lastValid(emaSlow),
		Series: emaSlow,
		State:  relativeState(lastClose, lastValid(emaSlow)),
		Note:   fmt.Sprintf("EMA%d vs price", cfg.EMA.Slow),
	}
	rep.Values["ema_long"] = IndicatorValue{
		Latest: lastValid(emaLong),
		Series: emaLong,
		State:  relativeState(lastClose, lastValid(emaLong)),
		Note:   fmt.Sprintf("EMA%d vs price", cfg.EMA.Long),
	}

	if cfg.RSI.Period <= 0 {
		cfg.RSI.Period = 14
	}
	if cfg.RSI.Overbought == 0 {
		cfg.RSI.Overbought = 70
	}
	if cfg.RSI.Oversold == 0 {
		cfg.RSI.Oversold = 30
	}
	rsiSeries := sanitizeSeries(talib.Rsi(closes, cfg.RSI.Period))
	rsiVal := lastValid(rsiSeries)
	state := "neutral"
	switch {
	case rsiVal >= cfg.RSI.Overbought:
		state = "overbought"
	case rsiVal <= cfg.RSI.Oversold:
		state = "oversold"
	}
	rep.Values["rsi"] = IndicatorValue{
		Latest: rsiVal,
		Series: rsiSeries,
		State:  state,
		Note:   fmt.Sprintf("period=%d thresholds=%.1f/%.1f", cfg.RSI.Period, cfg.RSI.Oversold, cfg.RSI.Overbought),
	}

	macd, signal, hist := talib.Macd(closes, 12, 26, 9)
	macdSeries := sanitizeSeries(macd)
	signalSeries := sanitizeSeries(signal)
	histSeries := sanitizeSeries(hist)
	macdNote := fmt.Sprintf("signal=%.4f hist=%.4f", lastValid(signalSeries), lastValid(histSeries))
	macdState := polarityState(lastValid(histSeries))
	rep.Values["macd"] = IndicatorValue{
		Latest: lastValid(macdSeries),
		Series: histSeries,
		State:  macdState,
		Note:   macdNote,
	}

	rocSeries := sanitizeSeries(talib.Roc(closes, 9))
	rocVal := lastValid(rocSeries)
	rep.Values["roc"] = IndicatorValue{
		Latest: rocVal,
		Series: rocSeries,
		State:  polarityState(rocVal),
		Note:   "period=9",
	}

	k, d := talib.Stoch(highs, lows, closes, 14, 3, talib.SMA, 3, talib.SMA)
	kSeries := sanitizeSeries(k)
	dSeries := sanitizeSeries(d)
	rep.Values["stoch_k"] = IndicatorValue{
		Latest: lastValid(kSeries),
		Series: kSeries,
		State:  stochasticState(lastValid(kSeries)),
		Note:   fmt.Sprintf("d=%.2f", lastValid(dSeries)),
	}

	will := sanitizeSeries(talib.WillR(highs, lows, closes, 14))
	rep.Values["williams_r"] = IndicatorValue{
		Latest: lastValid(will),
		Series: will,
		State:  stochasticState(-lastValid(will)),
		Note:   "period=14",
	}

	atrSeries := sanitizeSeries(talib.Atr(highs, lows, closes, 14))
	rep.Values["atr"] = IndicatorValue{
		Latest: lastValid(atrSeries),
		Series: atrSeries,
		State:  "volatility",
		Note:   "period=14",
	}

	obv := sanitizeSeries(talib.Obv(closes, volumes))
	rep.Values["obv"] = IndicatorValue{
		Latest: lastValid(obv),
		Series: obv,
		State:  polarityState(lastValid(rocSeries)),
		Note:   "volume thrust",
	}

	wtmfiSettings := NormalizeWTMFISettings(cfg.WTMFI)
	wtmfiSeries := sanitizeSeries(WTMFIPostProcess(calcWTMFIHybridSeries(highs, lows, closes, volumes, wtmfiSettings), wtmfiSettings.SmoothLen))
	if len(wtmfiSeries) > 0 {
		wtmfiVal := lastValid(wtmfiSeries)
		wtmfiState := "neutral"
		switch {
		case wtmfiVal >= wtmfiSettings.Overbought:
			wtmfiState = "overbought"
		case wtmfiVal <= wtmfiSettings.Oversold:
			wtmfiState = "oversold"
		}
		rep.Values["wt_mfi_hybrid"] = IndicatorValue{
			Latest: wtmfiVal,
			Series: wtmfiSeries,
			State:  wtmfiState,
			Note: fmt.Sprintf(
				"len=%d/%d/%d mfi=%d wt=%.2f scale=%.2f",
				wtmfiSettings.ChannelLen,
				wtmfiSettings.AvgLen,
				wtmfiSettings.SmoothLen,
				wtmfiSettings.MFILen,
				wtmfiSettings.WTWeight,
				wtmfiSettings.MFIScale,
			),
		}
	}

	return rep, nil
}

func ComputeATRSeries(candles []market.Candle, period int) ([]float64, error) {
	if len(candles) == 0 {
		return nil, fmt.Errorf("no candles")
	}
	if period <= 0 {
		period = 14
	}
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	closes := make([]float64, len(candles))
	for i, c := range candles {
		highs[i] = c.High
		lows[i] = c.Low
		closes[i] = c.Close
	}
	series := sanitizeSeries(talib.Atr(highs, lows, closes, period))
	if len(series) == 0 {
		return nil, fmt.Errorf("atr series empty")
	}
	return series, nil
}

const (
	wtmfiChannelLen = 10
	wtmfiAvgLen     = 8
	wtmfiSmoothLen  = 5
	wtmfiMFILen     = 10
	wtmfiWeight     = 0.3
	wtmfiMFIScale   = 1.5
	wtmfiOverbought = 50.0
	wtmfiOversold   = -50.0

	wtmfiPostMult          = 1.2
	wtmfiPostUseSigmoid    = false
	wtmfiPostSigmoidGain   = 2.2
	wtmfiPostOscMax        = 60.0
	wtmfiPostOscMin        = -60.0
	wtmfiPostStepSize      = 6.6
	wtmfiPostQuantize      = true
	wtmfiPostStepMethod    = "ROUND"
)

func NormalizeWTMFISettings(in WTMFISettings) WTMFISettings {
	out := in
	if out.ChannelLen <= 0 {
		out.ChannelLen = wtmfiChannelLen
	}
	if out.AvgLen <= 0 {
		out.AvgLen = wtmfiAvgLen
	}
	if out.SmoothLen <= 0 {
		out.SmoothLen = wtmfiSmoothLen
	}
	if out.MFILen <= 0 {
		out.MFILen = wtmfiMFILen
	}
	if out.WTWeight <= 0 {
		out.WTWeight = wtmfiWeight
	}
	if out.MFIScale <= 0 {
		out.MFIScale = wtmfiMFIScale
	}
	if out.Overbought == 0 {
		out.Overbought = wtmfiOverbought
	}
	if out.Oversold == 0 {
		out.Oversold = wtmfiOversold
	}
	return out
}

func ComputeWTMFIHybridSeries(candles []market.Candle, settings WTMFISettings) []float64 {
	if len(candles) == 0 {
		return nil
	}
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	closes := make([]float64, len(candles))
	volumes := make([]float64, len(candles))
	for i, c := range candles {
		highs[i] = c.High
		lows[i] = c.Low
		closes[i] = c.Close
		volumes[i] = c.Volume
	}
	return calcWTMFIHybridSeries(highs, lows, closes, volumes, settings)
}

func calcWTMFIHybridSeries(highs, lows, closes, volumes []float64, settings WTMFISettings) []float64 {
	if len(closes) == 0 {
		return nil
	}
	settings = NormalizeWTMFISettings(settings)
	n := len(closes)
	src := make([]float64, n)
	for i := range closes {
		src[i] = (highs[i] + lows[i] + closes[i]) / 3
	}

	esa := talib.Ema(src, settings.ChannelLen)
	absDiff := make([]float64, n)
	for i := range src {
		absDiff[i] = math.Abs(src[i] - esa[i])
	}
	d := talib.Ema(absDiff, settings.ChannelLen)
	ci := make([]float64, n)
	for i := range src {
		denom := 0.015 * d[i]
		if denom == 0 {
			ci[i] = 0
			continue
		}
		ci[i] = (src[i] - esa[i]) / denom
	}
	wt1 := talib.Ema(ci, settings.AvgLen)
	wt2 := almaSeries(wt1, settings.SmoothLen, 0.85, 6)
	mfiSeries := talib.Mfi(highs, lows, closes, volumes, settings.MFILen)
	hybrid := make([]float64, n)
	for i := range hybrid {
		mfiVal := (mfiSeries[i] - 50) * settings.MFIScale
		hybrid[i] = settings.WTWeight*wt2[i] + (1-settings.WTWeight)*mfiVal
	}

	required := maxInt(settings.ChannelLen, settings.AvgLen, settings.SmoothLen, settings.MFILen) + 1
	if required > n {
		required = n
	}
	for i := 0; i < required; i++ {
		hybrid[i] = math.NaN()
	}
	return hybrid
}

func WTMFIPostProcess(series []float64, smoothLen int) []float64 {
	if len(series) == 0 {
		return nil
	}
	if smoothLen <= 0 {
		smoothLen = wtmfiSmoothLen
	}
	processed := make([]float64, len(series))
	for i, v := range series {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			processed[i] = math.NaN()
			continue
		}
		val := v * wtmfiPostMult
		if wtmfiPostUseSigmoid {
			val = wtmfiSigmoid(val, wtmfiPostSigmoidGain)
		}
		processed[i] = val
	}

	smoothed := almaSeries(processed, smoothLen, 0.85, 6)
	out := make([]float64, len(series))
	for i, v := range smoothed {
		if i < smoothLen-1 || math.IsNaN(v) || math.IsInf(v, 0) || math.IsNaN(processed[i]) {
			out[i] = math.NaN()
			continue
		}
		val := clamp(v, wtmfiPostOscMin, wtmfiPostOscMax)
		if wtmfiPostQuantize && wtmfiPostStepSize > 0 {
			val = quantizeStep(val, wtmfiPostStepSize, wtmfiPostStepMethod)
			val = clamp(val, wtmfiPostOscMin, wtmfiPostOscMax)
		}
		out[i] = val
	}
	return out
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

func wtmfiSigmoid(val, gain float64) float64 {
	scaled := val / 100.0
	sig := 2.0/(1.0+math.Exp(-gain*scaled)) - 1.0
	return sig * 100.0
}

func clamp(val, minVal, maxVal float64) float64 {
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

func quantizeStep(val, step float64, method string) float64 {
	if step <= 0 {
		return val
	}
	scaled := val / step
	absScaled := math.Abs(scaled)
	var steps float64
	switch method {
	case "FLOOR":
		steps = math.Floor(absScaled)
	default:
		steps = math.Round(absScaled)
	}
	quantized := steps * step
	if scaled < 0 {
		quantized = -quantized
	}
	return quantized
}

func maxInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	maxVal := values[0]
	for _, v := range values[1:] {
		if v > maxVal {
			maxVal = v
		}
	}
	return maxVal
}

func sanitizeSeries(src []float64) []float64 {
	out := make([]float64, 0, len(src))
	for _, v := range src {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out = append(out, round4(v))
	}
	return out
}

func trimEMALeadingZeros(series []float64) []float64 {
	start := 0
	for start < len(series) && almostZero(series[start]) {
		start++
	}
	return series[start:]
}

func almostZero(v float64) bool {
	return math.Abs(v) <= 1e-9
}

func lastValid(series []float64) float64 {
	for i := len(series) - 1; i >= 0; i-- {
		if !math.IsNaN(series[i]) && !math.IsInf(series[i], 0) {
			return series[i]
		}
	}
	return 0
}

func relativeState(price, ref float64) string {
	if ref == 0 {
		return "unknown"
	}
	switch {
	case price > ref*1.002:
		return "above"
	case price < ref*0.998:
		return "below"
	default:
		return "touch"
	}
}

func polarityState(v float64) string {
	switch {
	case v > 0:
		return "positive"
	case v < 0:
		return "negative"
	default:
		return "flat"
	}
}

func stochasticState(v float64) string {
	switch {
	case v >= 80:
		return "overbought"
	case v <= 20:
		return "oversold"
	default:
		return "neutral"
	}
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
