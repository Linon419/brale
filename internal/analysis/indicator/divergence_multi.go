package indicator

import (
	"math"

	talib "github.com/markcheno/go-talib"

	"brale/internal/market"
)

const (
	multiDivPivotPeriod    = 5
	multiDivMaxPivotPoints = 10
	multiDivMaxBars        = 100
	multiDivMinCount       = 1
	multiDivDontConfirm    = false
	multiDivSearchMode     = "regular_hidden"
	multiDivSource         = "close"
)

type MultiDivSignal struct {
	Indicator string `json:"indicator"`
	Type      string `json:"type"`
	Distance  int    `json:"distance"`
}

func ComputeMultiDivSignals(candles []market.Candle) []MultiDivSignal {
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
	rsi := talib.Rsi(closes, 14)
	stoch := smaSeries(stochFastK(closes, highs, lows, 14), 3)
	cci := talib.Cci(highs, lows, closes, 10)
	mom := talib.Mom(closes, 10)
	obv := talib.Obv(closes, volumes)
	vwmacd := diffSeries(vwmaSeries(closes, volumes, 12), vwmaSeries(closes, volumes, 26))
	cmf := cmfSeries(highs, lows, closes, volumes, 21)
	mfi := talib.Mfi(highs, lows, closes, volumes, 14)

	seriesList := []struct {
		name   string
		series []float64
	}{
		{name: "macd", series: macd},
		{name: "macd_hist", series: macdHist},
		{name: "rsi", series: rsi},
		{name: "stoch", series: stoch},
		{name: "cci", series: cci},
		{name: "mom", series: mom},
		{name: "obv", series: obv},
		{name: "vwmacd", series: vwmacd},
		{name: "cmf", series: cmf},
		{name: "mfi", series: mfi},
	}

	allowRegular := multiDivSearchMode == "regular" || multiDivSearchMode == "regular_hidden"
	allowHidden := multiDivSearchMode == "hidden" || multiDivSearchMode == "regular_hidden"

	signals := make([]MultiDivSignal, 0)
	total := 0
	for _, item := range seriesList {
		if len(item.series) == 0 {
			continue
		}
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
			total++
			signals = append(signals, MultiDivSignal{
				Indicator: item.name,
				Type:      divergenceTypeLabel(y),
				Distance:  divs[y],
			})
		}
	}

	if total < multiDivMinCount {
		return nil
	}
	return signals
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
