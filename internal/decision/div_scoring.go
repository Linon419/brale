package decision

import (
	"math"
	"sync"
	"time"
)

// Indicator groups
var (
	momentumIndicators = map[string]bool{
		"macd": true, "macd_hist": true, "rsi": true,
		"stoch": true, "cci": true, "mom": true,
	}
	volumeIndicators = map[string]bool{
		"obv": true, "vwmacd": true, "cmf": true, "mfi": true,
	}
)

// Base weights
const (
	baseMomentumWeight = 1.0
	baseVolumeWeight   = 2.0
	minWeightRatio     = 0.5
	maxWeightRatio     = 2.0
	halfLifeDays       = 30.0
	thresholdRatio     = 0.4
	minSamplesForAdapt = 20
)

// DivergenceRecord stores a single divergence signal for learning
type DivergenceRecord struct {
	Timestamp      time.Time `json:"timestamp"`
	Indicator      string    `json:"indicator"`
	Type           string    `json:"type"` // positive_regular, negative_regular, etc.
	Symbol         string    `json:"symbol"`
	Timeframe      string    `json:"timeframe"`
	Price          float64   `json:"price"`
	ATR            float64   `json:"atr"`
	DynamicSuccess bool      `json:"dynamic_success"`
	PriceMove      float64   `json:"price_move_pct"`
	TradeTriggered bool      `json:"trade_triggered"`
	TradeProfit    float64   `json:"trade_profit"`
	Validated      bool      `json:"validated"`
}

// DivScoreResult is the output of the scoring system
type DivScoreResult struct {
	Direction      string             `json:"direction"` // up, down, conflict, none
	BullishScore   float64            `json:"bullish_score"`
	BearishScore   float64            `json:"bearish_score"`
	BullishThresh  float64            `json:"bullish_threshold"`
	BearishThresh  float64            `json:"bearish_threshold"`
	ActiveSignals  []ScoredSignal     `json:"signals,omitempty"`
	Weights        map[string]float64 `json:"weights,omitempty"`
}

// ScoredSignal is a signal with its weight
type ScoredSignal struct {
	Indicator string  `json:"indicator"`
	Type      string  `json:"type"`
	Weight    float64 `json:"weight"`
	Distance  int     `json:"distance"`
}

// DivScorer calculates divergence scores with adaptive weights
type DivScorer struct {
	mu      sync.RWMutex
	weights map[string]float64
	records []DivergenceRecord
}

// NewDivScorer creates a new scorer with default weights
func NewDivScorer() *DivScorer {
	weights := make(map[string]float64)
	for ind := range momentumIndicators {
		weights[ind] = baseMomentumWeight
	}
	for ind := range volumeIndicators {
		weights[ind] = baseVolumeWeight
	}
	return &DivScorer{
		weights: weights,
		records: make([]DivergenceRecord, 0),
	}
}

// Score calculates the divergence score from signals
func (s *DivScorer) Score(signals []multiDivSignal) DivScoreResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(signals) == 0 {
		return DivScoreResult{Direction: "none"}
	}

	var bullishScore, bearishScore float64
	var bullishMax, bearishMax float64
	bullishSignals := make([]ScoredSignal, 0)
	bearishSignals := make([]ScoredSignal, 0)

	for _, sig := range signals {
		weight := s.getWeight(sig.Indicator)
		scored := ScoredSignal{
			Indicator: sig.Indicator,
			Type:      sig.Type,
			Weight:    weight,
			Distance:  sig.Distance,
		}

		isBullish := sig.Type == "positive_regular" || sig.Type == "positive_hidden"
		if isBullish {
			bullishScore += weight
			bullishMax += weight
			bullishSignals = append(bullishSignals, scored)
		} else {
			bearishScore += weight
			bearishMax += weight
			bearishSignals = append(bearishSignals, scored)
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

	allSignals := append(bullishSignals, bearishSignals...)

	return DivScoreResult{
		Direction:     direction,
		BullishScore:  roundFloat(bullishScore, 2),
		BearishScore:  roundFloat(bearishScore, 2),
		BullishThresh: roundFloat(bullishThresh, 2),
		BearishThresh: roundFloat(bearishThresh, 2),
		ActiveSignals: allSignals,
		Weights:       s.copyWeights(),
	}
}

// AddRecord adds a divergence record for learning
func (s *DivScorer) AddRecord(rec DivergenceRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
}

// UpdateWeights recalculates weights based on historical records
func (s *DivScorer) UpdateWeights() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	indicatorStats := make(map[string]*indicatorStat)

	for _, rec := range s.records {
		if !rec.Validated {
			continue
		}

		stat, ok := indicatorStats[rec.Indicator]
		if !ok {
			stat = &indicatorStat{}
			indicatorStats[rec.Indicator] = stat
		}

		decay := calcDecay(now, rec.Timestamp)
		stat.totalWeight += decay

		if rec.DynamicSuccess {
			stat.dynamicSuccessWeight += decay
		}
		if rec.TradeTriggered {
			stat.tradeWeight += decay
			if rec.TradeProfit > 0 {
				stat.tradeProfitWeight += decay
			}
		}
	}

	for ind, stat := range indicatorStats {
		if stat.totalWeight < minSamplesForAdapt {
			continue
		}

		dynamicRate := stat.dynamicSuccessWeight / stat.totalWeight
		tradeRate := 0.5
		if stat.tradeWeight > 0 {
			tradeRate = stat.tradeProfitWeight / stat.tradeWeight
		}

		// Mixed: 0.3*dynamic + 0.7*trade (adjust if trade data insufficient)
		tradeRatio := 0.7
		if stat.tradeWeight < 10 {
			tradeRatio = 0.3
		}
		combinedRate := (1-tradeRatio)*dynamicRate + tradeRatio*tradeRate

		baseWeight := s.getBaseWeight(ind)
		newWeight := baseWeight * (0.5 + combinedRate)
		newWeight = clampWeight(newWeight, baseWeight)
		s.weights[ind] = roundFloat(newWeight, 4)
	}
}

// GetRecords returns all records (for persistence)
func (s *DivScorer) GetRecords() []DivergenceRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DivergenceRecord, len(s.records))
	copy(out, s.records)
	return out
}

// LoadRecords loads records (from persistence)
func (s *DivScorer) LoadRecords(records []DivergenceRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = records
}

func (s *DivScorer) getWeight(indicator string) float64 {
	if w, ok := s.weights[indicator]; ok {
		return w
	}
	return s.getBaseWeight(indicator)
}

func (s *DivScorer) getBaseWeight(indicator string) float64 {
	if momentumIndicators[indicator] {
		return baseMomentumWeight
	}
	if volumeIndicators[indicator] {
		return baseVolumeWeight
	}
	return 1.0
}

func (s *DivScorer) copyWeights() map[string]float64 {
	out := make(map[string]float64, len(s.weights))
	for k, v := range s.weights {
		out[k] = v
	}
	return out
}

type indicatorStat struct {
	totalWeight          float64
	dynamicSuccessWeight float64
	tradeWeight          float64
	tradeProfitWeight    float64
}

func calcDecay(now, ts time.Time) float64 {
	days := now.Sub(ts).Hours() / 24
	if days < 0 {
		days = 0
	}
	return math.Pow(0.5, days/halfLifeDays)
}

func clampWeight(w, base float64) float64 {
	minW := base * minWeightRatio
	maxW := base * maxWeightRatio
	if w < minW {
		return minW
	}
	if w > maxW {
		return maxW
	}
	return w
}
