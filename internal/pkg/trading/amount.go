package trading

import "math"

// CalcCloseAmount computes close size using ratios over the initial position.
// base = initialAmount when isInitialRatio=true (fallback to currentAmount),
// then clamps to currentAmount to avoid over-closing.
func CalcCloseAmount(currentAmount, initialAmount, ratio float64, isInitialRatio bool) float64 {
	if ratio <= 0 || currentAmount <= 0 {
		return 0
	}
	if ratio > 1 {
		ratio = 1
	}

	base := currentAmount
	if isInitialRatio && initialAmount > 0 {
		base = initialAmount
	}

	target := ceilToDecimals(base*ratio, 2)
	return math.Min(target, currentAmount)
}

// ceilToDecimals rounds a number up to the given decimal places.
// Using ceiling helps counter exchanges that truncate amounts downward, reducing tiny leftovers.
func ceilToDecimals(v float64, decimals int) float64 {
	if decimals <= 0 {
		return math.Ceil(v)
	}
	factor := math.Pow10(decimals)
	return math.Ceil(v*factor) / factor
}
