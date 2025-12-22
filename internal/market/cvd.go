package market

import "github.com/shopspring/decimal"

type CVDMetrics struct {
	Value      decimal.Decimal
	Momentum   decimal.Decimal
	Normalized decimal.Decimal
	Divergence string
	PeakFlip   string
}

// ComputeCVD calculates a CVD snapshot from taker buy/sell volumes.
// Output meanings:
//   - Value: cumulative sum of (taker_buy - taker_sell) across the window.
//   - Momentum: Value minus the value 6 bars ago (0 when insufficient bars).
//   - Normalized: (Value - min) / (max - min) across the CVD series, 0.5 when flat.
//   - Divergence: "bearish" if price rises while CVD falls vs 6 bars ago;
//     "bullish" if price falls while CVD rises; otherwise "neutral".
//   - PeakFlip: "top" if last CVD is below previous and previous is above prior;
//     "bottom" if last is above previous and previous below prior; else "none".
func ComputeCVD(candles []Candle) (CVDMetrics, bool) {
	if len(candles) == 0 {
		return CVDMetrics{}, false
	}
	cvd := make([]decimal.Decimal, 0, len(candles))
	closes := make([]decimal.Decimal, 0, len(candles))
	cumulative := decimal.Zero
	for _, c := range candles {
		buy := decimal.NewFromFloat(c.TakerBuyVolume)
		sell := decimal.NewFromFloat(c.TakerSellVolume)
		cumulative = cumulative.Add(buy.Sub(sell))
		cvd = append(cvd, cumulative)
		closes = append(closes, decimal.NewFromFloat(c.Close))
	}

	last := cvd[len(cvd)-1]
	momentum := decimal.Zero
	if len(cvd) > 6 {
		momentum = last.Sub(cvd[len(cvd)-6])
	}

	minVal := cvd[0]
	maxVal := cvd[0]
	for _, v := range cvd[1:] {
		if v.LessThan(minVal) {
			minVal = v
		}
		if v.GreaterThan(maxVal) {
			maxVal = v
		}
	}

	norm := decimal.NewFromFloat(0.5)
	if maxVal.GreaterThan(minVal) {
		norm = last.Sub(minVal).Div(maxVal.Sub(minVal))
	}

	priceNow := closes[len(closes)-1]
	pricePrev := closes[0]
	cvdPrev := cvd[0]
	if len(closes) > 6 {
		pricePrev = closes[len(closes)-6]
		cvdPrev = cvd[len(cvd)-6]
	}

	divergence := "neutral"
	if priceNow.GreaterThan(pricePrev) && last.LessThan(cvdPrev) {
		divergence = "down"
	} else if priceNow.LessThan(pricePrev) && last.GreaterThan(cvdPrev) {
		divergence = "up"
	}

	peakFlip := "none"
	if len(cvd) > 3 {
		a := cvd[len(cvd)-1]
		b := cvd[len(cvd)-2]
		c := cvd[len(cvd)-3]
		if a.LessThan(b) && b.GreaterThan(c) {
			peakFlip = "local_top"
		} else if a.GreaterThan(b) && b.LessThan(c) {
			peakFlip = "local_bottom"
		}
	}

	return CVDMetrics{
		Value:      last,
		Momentum:   momentum,
		Normalized: norm,
		Divergence: divergence,
		PeakFlip:   peakFlip,
	}, true
}
