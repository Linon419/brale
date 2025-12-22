package freqtrade

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"brale/internal/config"
	"brale/internal/gateway/exchange"
	"brale/internal/logger"
	symbolpkg "brale/internal/pkg/symbol"
)

type Adapter struct {
	client *Client
	cfg    *config.FreqtradeConfig
}

func NewAdapter(client *Client, cfg *config.FreqtradeConfig) *Adapter {
	return &Adapter{
		client: client,
		cfg:    cfg,
	}
}

func (a *Adapter) Name() string {
	return "freqtrade"
}

func (a *Adapter) OpenPosition(ctx context.Context, req exchange.OpenRequest) (*exchange.OpenResult, error) {
	payload := ForceEnterPayload{
		Pair:        a.toFreqtradePair(req.Symbol),
		Side:        req.Side,
		StakeAmount: req.Amount,
		OrderType:   req.OrderType,
	}

	if req.Price > 0 {
		payload.Price = &req.Price
	}
	if req.Leverage > 0 {
		payload.Leverage = req.Leverage
	}

	logger.Infof("Adapter open position : %s %s %.2f", req.Symbol, req.Side, req.Amount)

	resp, err := a.client.ForceEnter(ctx, payload)
	if err != nil {
		// Try to enrich error context with available balance to make 4xx/5xx easier to diagnose.
		avail := a.lookupAvailableStake(ctx)
		logger.Errorf("freqtrade forceenter failed (pair=%s side=%s stake=%.4f lev=%.2f avail=%.4f): %v", payload.Pair, payload.Side, payload.StakeAmount, payload.Leverage, avail, err)
		return nil, fmt.Errorf("freqtrade forceenter failed (stake=%.4f, leverage=%.2f, available=%.4f): %w", payload.StakeAmount, payload.Leverage, avail, err)
	}

	return &exchange.OpenResult{
		PositionID: strconv.Itoa(resp.TradeID),
	}, nil
}

func (a *Adapter) ClosePosition(ctx context.Context, req exchange.CloseRequest) error {
	tradeID, ftRemain, err := a.resolveCloseTarget(ctx, req)
	if err != nil {
		return err
	}
	amount := clampCloseAmount(req.Amount, ftRemain)

	logger.Infof("Adapter ClosePosition: %s (TradeID: %s) amount=%.6f ftRemain=%.6f", req.Symbol, tradeID, amount, ftRemain)

	if err := a.forceExitWithRetry(ctx, tradeID, req.Symbol, amount); err != nil {
		return err
	}
	return nil
}

func (a *Adapter) resolveCloseTarget(ctx context.Context, req exchange.CloseRequest) (string, float64, error) {
	tradeID := strings.TrimSpace(req.PositionID)
	if tradeID != "" {
		return tradeID, a.fetchRemoteRemaining(ctx, tradeID), nil
	}
	if req.Symbol == "" {
		return "", 0, fmt.Errorf("close request missing symbol")
	}

	trades, err := a.client.ListTrades(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("failed to list trades to find close target: %w", err)
	}
	for _, t := range trades {
		if strings.EqualFold(a.fromFreqtradePair(t.Pair), req.Symbol) && t.IsOpen {
			return strconv.Itoa(t.ID), t.Amount, nil
		}
	}
	return "", 0, fmt.Errorf("no active trade found for %s to close", req.Symbol)
}

func clampCloseAmount(reqAmount, ftRemain float64) float64 {
	amount := reqAmount
	if amount <= 0 && ftRemain > 0 {
		amount = ftRemain
	}
	if ftRemain > 0 && amount > ftRemain {
		amount = ftRemain
	}
	if amount < 0 {
		amount = 0
	}
	return amount
}

func (a *Adapter) fetchRemoteRemaining(ctx context.Context, tradeID string) float64 {
	if tradeID == "" {
		return 0
	}
	id, err := strconv.Atoi(tradeID)
	if err != nil {
		logger.Warnf("freqtrade fetch remain invalid tradeID=%s: %v", tradeID, err)
		return 0
	}
	tr, err := a.client.GetOpenTrade(ctx, id)
	if err != nil && !errors.Is(err, errTradeNotFound) {
		logger.Warnf("freqtrade get open trade failed id=%s: %v", tradeID, err)
	}
	if tr == nil {
		return 0
	}
	return tr.Amount
}

func (a *Adapter) forceExitWithRetry(ctx context.Context, tradeID, symbol string, amount float64) error {
	payload := ForceExitPayload{TradeID: tradeID}
	call := func(am float64) error {
		pl := payload
		if am > 0 {
			pl.Amount = am
		}
		return a.client.ForceExit(ctx, pl)
	}

	err := call(amount)
	if err == nil {
		return nil
	}

	logger.Errorf("freqtrade forceexit failed (symbol=%s tradeID=%s amount=%.4f): %v", symbol, tradeID, amount, err)
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "remaining amount") {
		return fmt.Errorf("freqtrade forceexit failed: %w", err)
	}

	freshRemain := a.fetchRemoteRemaining(ctx, tradeID)
	if freshRemain <= 0 {
		return fmt.Errorf("freqtrade forceexit failed: %w", err)
	}

	logger.Warnf("freqtrade retry close with remote amount %.6f (symbol=%s tradeID=%s)", freshRemain, symbol, tradeID)
	if retryErr := call(freshRemain); retryErr != nil {
		logger.Errorf("freqtrade forceexit retry failed (symbol=%s tradeID=%s amount=%.4f): %v", symbol, tradeID, freshRemain, retryErr)
		return fmt.Errorf("freqtrade forceexit failed: %w", err)
	}
	return nil
}

func (a *Adapter) ListOpenPositions(ctx context.Context) ([]exchange.Position, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("freqtrade adapter not initialized")
	}
	trades, err := a.client.ListTrades(ctx)
	if err != nil {
		logger.Errorf("freqtrade list trades failed: %v", err)
		return nil, err
	}
	positions := make([]exchange.Position, 0, len(trades))
	for _, tr := range trades {
		if p := a.tradeToExchangePosition(&tr); p != nil {
			positions = append(positions, *p)
		}
	}
	return positions, nil
}

func (a *Adapter) GetPosition(ctx context.Context, positionID string) (*exchange.Position, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("freqtrade adapter not initialized")
	}
	if positionID == "" {
		return nil, fmt.Errorf("positionID required")
	}
	id, err := strconv.Atoi(positionID)
	if err != nil {
		return nil, fmt.Errorf("invalid trade ID: %s", positionID)
	}

	tr, err := a.client.GetTrade(ctx, id)
	if err != nil {
		logger.Errorf("freqtrade get trade failed id=%d: %v", id, err)
		return nil, err
	}
	if tr == nil {
		return nil, nil
	}
	pos := a.tradeToExchangePosition(tr)
	return pos, nil
}

func (a *Adapter) GetBalance(ctx context.Context) (exchange.Balance, error) {
	if a == nil || a.client == nil {
		return exchange.Balance{}, fmt.Errorf("freqtrade adapter not initialized")
	}
	bal, err := a.client.GetBalance(ctx)
	if err != nil {
		logger.Errorf("freqtrade get balance failed: %v", err)
	}
	return bal, err
}

func (a *Adapter) GetPrice(ctx context.Context, symbol string) (exchange.PriceQuote, error) {
	return exchange.PriceQuote{}, fmt.Errorf("GetPrice not implemented for freqtrade")
}

func (a *Adapter) lookupAvailableStake(ctx context.Context) float64 {
	if a == nil {
		return 0
	}
	bal, err := a.GetBalance(ctx)
	if err != nil {
		return 0
	}
	return bal.Available
}

func (a *Adapter) toFreqtradePair(sym string) string {
	stakeCurrency := ""
	if a.cfg != nil {
		stakeCurrency = a.cfg.StakeCurrency
	}
	return symbolpkg.Freqtrade(stakeCurrency).ToExchange(sym)
}

func (a *Adapter) fromFreqtradePair(ftPair string) string {
	stakeCurrency := ""
	if a.cfg != nil {
		stakeCurrency = a.cfg.StakeCurrency
	}
	return symbolpkg.Freqtrade(stakeCurrency).FromExchange(ftPair)
}

func (a *Adapter) tradeToExchangePosition(t *Trade) *exchange.Position {
	if t == nil {
		return nil
	}
	side := "long"
	if t.IsShort || strings.Contains(strings.ToLower(t.Side), "short") {
		side = "short"
	}

	openedAt := parseTradeTime(t.OpenDate)
	initialAmt := t.Amount
	if t.AmountRequested > 0 {
		initialAmt = t.AmountRequested
	}

	return &exchange.Position{
		ID:            strconv.Itoa(t.ID),
		Symbol:        a.fromFreqtradePair(t.Pair),
		Side:          side,
		Amount:        t.Amount,
		InitialAmount: initialAmt,
		EntryPrice:    t.OpenRate,
		Leverage:      t.Leverage,
		StakeAmount:   t.StakeAmount,
		OpenedAt:      openedAt,
		IsOpen:        t.IsOpen,

		UnrealizedPnL:      t.ProfitAbs,
		UnrealizedPnLRatio: t.ProfitRatio,
		CurrentPrice:       t.CurrentRate,
	}
}

func parseTradeTime(raw string) time.Time {
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}
