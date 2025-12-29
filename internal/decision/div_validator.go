package decision

import (
	"sync"
	"time"

	"brale/internal/market"
)

// Validation window bars per timeframe
var validationWindowBars = map[string]int{
	"15m": 20, // 5 hours
	"1h":  12, // 12 hours
	"4h":  8,  // 32 hours
}

const (
	atrMultiplier        = 1.5
	defaultValidationWin = 12
)

// PendingValidation represents a divergence signal awaiting validation
type PendingValidation struct {
	Record       DivergenceRecord
	TargetPrice  float64 // Price target for success
	WindowBars   int     // Number of bars to wait
	BarsElapsed  int     // Bars counted so far
	HighestPrice float64 // For bullish: track highest
	LowestPrice  float64 // For bearish: track lowest
}

// DivValidator tracks and validates divergence signals
type DivValidator struct {
	mu       sync.Mutex
	pending  map[string][]PendingValidation // key: symbol_timeframe
	scorer   *DivScorer
	onUpdate func() // callback when weights updated
}

// NewDivValidator creates a new validator
func NewDivValidator(scorer *DivScorer) *DivValidator {
	return &DivValidator{
		pending: make(map[string][]PendingValidation),
		scorer:  scorer,
	}
}

// SetUpdateCallback sets the callback for when weights are updated
func (v *DivValidator) SetUpdateCallback(fn func()) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.onUpdate = fn
}

// RegisterSignal registers a new divergence signal for validation
func (v *DivValidator) RegisterSignal(
	signal multiDivSignal,
	symbol, timeframe string,
	price, atr float64,
) {
	v.mu.Lock()
	defer v.mu.Unlock()

	key := symbol + "_" + timeframe
	isBullish := signal.Type == "positive_regular" || signal.Type == "positive_hidden"

	windowBars := defaultValidationWin
	if w, ok := validationWindowBars[timeframe]; ok {
		windowBars = w
	}

	var targetPrice float64
	if isBullish {
		targetPrice = price * (1 + atr*atrMultiplier/price)
	} else {
		targetPrice = price * (1 - atr*atrMultiplier/price)
	}

	pv := PendingValidation{
		Record: DivergenceRecord{
			Timestamp: time.Now().UTC(),
			Indicator: signal.Indicator,
			Type:      signal.Type,
			Symbol:    symbol,
			Timeframe: timeframe,
			Price:     price,
			ATR:       atr,
		},
		TargetPrice:  targetPrice,
		WindowBars:   windowBars,
		BarsElapsed:  0,
		HighestPrice: price,
		LowestPrice:  price,
	}

	v.pending[key] = append(v.pending[key], pv)
}

// OnNewCandle processes a new candle and validates pending signals
func (v *DivValidator) OnNewCandle(symbol, timeframe string, candle market.Candle) {
	v.mu.Lock()
	defer v.mu.Unlock()

	key := symbol + "_" + timeframe
	pending, ok := v.pending[key]
	if !ok || len(pending) == 0 {
		return
	}

	remaining := make([]PendingValidation, 0, len(pending))
	for _, pv := range pending {
		pv.BarsElapsed++

		// Update price extremes
		if candle.High > pv.HighestPrice {
			pv.HighestPrice = candle.High
		}
		if candle.Low < pv.LowestPrice {
			pv.LowestPrice = candle.Low
		}

		// Check if validation complete
		if pv.BarsElapsed >= pv.WindowBars {
			rec := pv.Record
			isBullish := rec.Type == "positive_regular" || rec.Type == "positive_hidden"

			if isBullish {
				rec.DynamicSuccess = pv.HighestPrice >= pv.TargetPrice
				rec.PriceMove = (pv.HighestPrice - rec.Price) / rec.Price * 100
			} else {
				rec.DynamicSuccess = pv.LowestPrice <= pv.TargetPrice
				rec.PriceMove = (rec.Price - pv.LowestPrice) / rec.Price * 100
			}
			rec.Validated = true

			v.scorer.AddRecord(rec)
			continue
		}

		remaining = append(remaining, pv)
	}

	v.pending[key] = remaining

	// Periodically update weights
	if len(v.scorer.GetRecords())%10 == 0 && len(v.scorer.GetRecords()) > 0 {
		v.scorer.UpdateWeights()
		if v.onUpdate != nil {
			v.onUpdate()
		}
	}
}

// MarkTradeResult updates a record with trade result
func (v *DivValidator) MarkTradeResult(
	symbol, timeframe, indicator, divType string,
	ts time.Time,
	profit float64,
) {
	records := v.scorer.GetRecords()
	for i := range records {
		rec := &records[i]
		if rec.Symbol == symbol &&
			rec.Timeframe == timeframe &&
			rec.Indicator == indicator &&
			rec.Type == divType &&
			rec.Timestamp.Sub(ts).Abs() < time.Hour {
			rec.TradeTriggered = true
			rec.TradeProfit = profit
			break
		}
	}
	v.scorer.LoadRecords(records)
}

// GetPendingCount returns number of pending validations
func (v *DivValidator) GetPendingCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	count := 0
	for _, list := range v.pending {
		count += len(list)
	}
	return count
}
