package binance

import (
	"context"
	"fmt"
	"strings"

	"brale/internal/market"
)

// GetFundingRate 获取最新资金费率（例如 0.0001 即 0.01%）
func (s *Source) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	if s == nil || s.client == nil {
		return 0, fmt.Errorf("binance source not initialized")
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return 0, fmt.Errorf("symbol is required")
	}
	res, err := s.client.NewPremiumIndexService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, err
	}
	for _, entry := range res {
		if entry == nil {
			continue
		}
		if strings.EqualFold(entry.Symbol, symbol) {
			return parseFloat(entry.LastFundingRate), nil
		}
	}
	if len(res) > 0 {
		return parseFloat(res[0].LastFundingRate), nil
	}
	return 0, fmt.Errorf("funding rate not available for %s", symbol)
}

// GetOpenInterestHistory 获取 OI 历史数据
func (s *Source) GetOpenInterestHistory(ctx context.Context, symbol, period string, limit int) ([]market.OpenInterestPoint, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("binance source not initialized")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 500 {
		limit = 500
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	period = strings.ToLower(strings.TrimSpace(period))
	if symbol == "" || period == "" {
		return nil, fmt.Errorf("symbol and period are required")
	}
	svc := s.client.NewOpenInterestStatisticsService().Symbol(symbol).Period(period).Limit(limit)
	stats, err := svc.Do(ctx)
	if err != nil {
		return nil, err
	}
	points := make([]market.OpenInterestPoint, 0, len(stats))
	for _, item := range stats {
		if item == nil {
			continue
		}
		points = append(points, market.OpenInterestPoint{
			Symbol:               item.Symbol,
			SumOpenInterest:      parseFloat(item.SumOpenInterest),
			SumOpenInterestValue: parseFloat(item.SumOpenInterestValue),
			Timestamp:            item.Timestamp,
		})
	}
	return points, nil
}
