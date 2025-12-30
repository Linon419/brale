package decision

import (
	"math"

	"brale/internal/market"
	"brale/internal/scheduler"

	"github.com/markcheno/go-talib"
)

// LeverageConfig holds configuration for ATR-based leverage calculation.
type LeverageConfig struct {
	// ATRPeriod is the period for ATR calculation (default: 14).
	ATRPeriod int `json:"atr_period,omitempty"`
	// ATRTimeframe specifies the timeframe for ATR (e.g., "1d", "1h", "4h").
	// Default is "1d" (daily).
	ATRTimeframe string `json:"atr_timeframe,omitempty"`
	// MaxLeverage caps the computed leverage (default: 50).
	MaxLeverage int `json:"max_leverage,omitempty"`
	// MinLeverage is the minimum leverage (default: 1).
	MinLeverage int `json:"min_leverage,omitempty"`
	// StopLossRiskPct is the max risk per trade as % of total capital (default: 5.0).
	// This is used for "以损订仓" (position sizing by stop loss).
	StopLossRiskPct float64 `json:"stop_loss_risk_pct,omitempty"`
}

// LeverageResult contains the computed leverage and position sizing info.
type LeverageResult struct {
	// Leverage is the computed leverage based on ATR.
	Leverage int `json:"leverage"`
	// ATRValue is the current ATR value.
	ATRValue float64 `json:"atr_value"`
	// MaxATR24h is the maximum ATR in the past 24 hours.
	MaxATR24h float64 `json:"max_atr_24h"`
	// CurrentPrice is the current close price.
	CurrentPrice float64 `json:"current_price"`
	// PositionSizeUSD is the computed position size based on stop-loss risk.
	// Formula: position_size = (capital * risk_pct) / stop_distance_pct * leverage
	PositionSizeUSD float64 `json:"position_size_usd,omitempty"`
	// StopDistancePct is the stop-loss distance as percentage of entry price.
	StopDistancePct float64 `json:"stop_distance_pct,omitempty"`
}

// DefaultLeverageConfig returns the default configuration.
func DefaultLeverageConfig() LeverageConfig {
	return LeverageConfig{
		ATRPeriod:       14,
		ATRTimeframe:    "1d",
		MaxLeverage:     50,
		MinLeverage:     1,
		StopLossRiskPct: 5.0,
	}
}

// NormalizeLeverageConfig fills in default values for missing fields.
func NormalizeLeverageConfig(cfg LeverageConfig) LeverageConfig {
	if cfg.ATRPeriod <= 0 {
		cfg.ATRPeriod = 14
	}
	if cfg.ATRTimeframe == "" {
		cfg.ATRTimeframe = "1d"
	}
	if cfg.MaxLeverage <= 0 {
		cfg.MaxLeverage = 50
	}
	if cfg.MinLeverage <= 0 {
		cfg.MinLeverage = 1
	}
	if cfg.StopLossRiskPct <= 0 {
		cfg.StopLossRiskPct = 5.0
	}
	return cfg
}

// CalcATRLeverage computes leverage based on the formula: leverage = close / max_atr_24h
// This follows the PineScript logic:
//
//	atr_value = ta.atr(atr_length)  // ATR on specified timeframe
//	bars_in_24h = math.ceil(24 * 60 / timeframe.multiplier)
//	max_atr_24h = ta.highest(atr_value, bars_in_24h)
//	leverage = close / max_atr_24h
func CalcATRLeverage(candles []market.Candle, cfg LeverageConfig) (LeverageResult, error) {
	cfg = NormalizeLeverageConfig(cfg)

	result := LeverageResult{
		Leverage: cfg.MinLeverage,
	}

	if len(candles) == 0 {
		return result, nil
	}

	// Extract OHLC data
	n := len(candles)
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	for i, c := range candles {
		highs[i] = c.High
		lows[i] = c.Low
		closes[i] = c.Close
	}

	// Compute ATR series
	atrSeries := talib.Atr(highs, lows, closes, cfg.ATRPeriod)
	if len(atrSeries) == 0 {
		return result, nil
	}

	// Get current price (last close)
	currentPrice := closes[n-1]
	if currentPrice <= 0 {
		return result, nil
	}
	result.CurrentPrice = currentPrice

	// Get current ATR value
	currentATR := lastValidFloat(atrSeries)
	if currentATR <= 0 {
		return result, nil
	}
	result.ATRValue = roundFloat(currentATR, 4)

	// Calculate bars in 24 hours based on timeframe
	barsIn24h := barsInPeriod(cfg.ATRTimeframe, 24*60)

	// Calculate max ATR in past 24 hours (or available bars)
	lookback := barsIn24h
	if lookback > len(atrSeries) {
		lookback = len(atrSeries)
	}
	if lookback < 1 {
		lookback = 1
	}

	maxATR24h := highestValue(atrSeries, lookback)
	if maxATR24h <= 0 {
		maxATR24h = currentATR
	}
	result.MaxATR24h = roundFloat(maxATR24h, 4)

	// Calculate leverage: leverage = close / max_atr_24h
	leverage := currentPrice / maxATR24h
	leverageInt := int(math.Round(leverage))

	// Clamp to min/max bounds
	if leverageInt < cfg.MinLeverage {
		leverageInt = cfg.MinLeverage
	}
	if leverageInt > cfg.MaxLeverage {
		leverageInt = cfg.MaxLeverage
	}

	result.Leverage = leverageInt
	return result, nil
}

// CalcPositionSizeByStopLoss calculates position size using "以损订仓" (position sizing by stop loss).
// Formula: position_size_usd = (capital * risk_pct%) / stop_distance_pct * (1 / leverage)
//
// Where:
//   - capital: total account equity
//   - risk_pct: max risk per trade (e.g., 5% means losing 5% of capital if SL is hit)
//   - stop_distance_pct: distance from entry to stop-loss as percentage
//   - leverage: the trading leverage
//
// The idea is: if SL is hit, we lose exactly risk_pct% of capital.
// Loss = position_size * stop_distance_pct / leverage
// capital * risk_pct% = position_size * stop_distance_pct / leverage
// position_size = capital * risk_pct% * leverage / stop_distance_pct
func CalcPositionSizeByStopLoss(capital float64, riskPct float64, stopDistancePct float64, leverage int) float64 {
	if capital <= 0 || riskPct <= 0 || stopDistancePct <= 0 || leverage <= 0 {
		return 0
	}

	// Max loss amount = capital * riskPct/100
	maxLoss := capital * (riskPct / 100.0)

	// position_size = maxLoss * leverage / stopDistancePct
	// With leverage L, if price moves stopDistancePct%, the loss is:
	// loss = position_value * stopDistancePct / leverage
	// We want: loss = maxLoss
	// So: position_value = maxLoss * leverage / (stopDistancePct/100)
	positionSize := maxLoss / (stopDistancePct / 100.0)

	return roundFloat(positionSize, 2)
}

// CalcLeverageWithPositionSize computes both leverage and position size based on ATR and stop-loss risk.
func CalcLeverageWithPositionSize(
	candles []market.Candle,
	cfg LeverageConfig,
	capital float64,
	stopDistancePct float64,
) (LeverageResult, error) {
	result, err := CalcATRLeverage(candles, cfg)
	if err != nil {
		return result, err
	}

	if capital > 0 && stopDistancePct > 0 {
		cfg = NormalizeLeverageConfig(cfg)
		result.StopDistancePct = stopDistancePct
		result.PositionSizeUSD = CalcPositionSizeByStopLoss(
			capital,
			cfg.StopLossRiskPct,
			stopDistancePct,
			result.Leverage,
		)
	}

	return result, nil
}

// barsInPeriod calculates how many bars fit in a given period (in minutes).
func barsInPeriod(interval string, periodMinutes int) int {
	dur, ok := scheduler.ParseIntervalDuration(interval)
	if !ok || dur.Minutes() <= 0 {
		// Default to daily: 1 bar = 1440 minutes
		return 1
	}
	bars := int(math.Ceil(float64(periodMinutes) / dur.Minutes()))
	if bars < 1 {
		bars = 1
	}
	return bars
}

// highestValue returns the highest value in the last n elements of a series.
func highestValue(series []float64, n int) float64 {
	if len(series) == 0 || n <= 0 {
		return 0
	}
	start := len(series) - n
	if start < 0 {
		start = 0
	}
	maxVal := -math.MaxFloat64
	for i := start; i < len(series); i++ {
		v := series[i]
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == -math.MaxFloat64 {
		return 0
	}
	return maxVal
}

// lastValidFloat returns the last valid (non-NaN, non-Inf) value in a series.
func lastValidFloat(series []float64) float64 {
	for i := len(series) - 1; i >= 0; i-- {
		v := series[i]
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return v
		}
	}
	return 0
}
