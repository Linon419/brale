package freqtrade

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/logger"
)

func (m *Manager) evaluateTiers(ctx context.Context, priceFn func(symbol string) TierPriceQuote) {
	if m.posRepo == nil || priceFn == nil {
		return
	}
	positions := m.cachePositions()
	if len(positions) == 0 {
		positions = m.refreshCache(ctx)
	}
	for _, p := range positions {
		symbol := strings.ToUpper(strings.TrimSpace(p.Order.Symbol))
		if symbol == "" {
			continue
		}
		if m.hasPendingExit(p.Order.FreqtradeID) {
			continue
		}
		if p.Order.Status != database.LiveOrderStatusOpen && p.Order.Status != database.LiveOrderStatusPartial {
			continue
		}
		if p.Tiers.IsPlaceholder || !hasCompleteTier(p.Tiers) {
			continue
		}
		quote := priceFn(symbol)
		if quote.isEmpty() || quote.Last <= 0 {
			m.reportMissingPrice(symbol)
			continue
		}
		m.clearMissingPrice(symbol)
		side := strings.ToLower(strings.TrimSpace(p.Order.Side))
		lock := getPositionLock(p.Order.FreqtradeID)
		lock.Lock()
		m.evaluateOne(ctx, side, quote, p)
		lock.Unlock()
	}
}

func (m *Manager) reportMissingPrice(symbol string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.missingPrice == nil {
		m.missingPrice = make(map[string]bool)
	}
	if m.missingPrice[symbol] {
		m.mu.Unlock()
		return
	}
	m.missingPrice[symbol] = true
	m.mu.Unlock()

	logger.Warnf("自动平仓监控暂停：缺少 WSS 最新价 %s", symbol)
	m.notify("自动平仓监控暂停 ⚠️",
		fmt.Sprintf("标的: %s", symbol),
		"原因: 缺少 WSS 最新价，已暂停 tier/止损/止盈监控",
	)
}

func (m *Manager) clearMissingPrice(symbol string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.missingPrice != nil {
		delete(m.missingPrice, symbol)
	}
	m.mu.Unlock()
}

func (m *Manager) evaluateOne(ctx context.Context, side string, quote TierPriceQuote, p database.LiveOrderWithTiers) {
	stop := p.Tiers.StopLoss
	tp := p.Tiers.TakeProfit

	if price, hit := priceForStopLoss(side, quote, stop); hit && stop > 0 {
		m.startPendingClose(ctx, p, "stop_loss", price, 1, database.OperationStopLoss)
		return
	}
	if price, hit := priceForTakeProfit(side, quote, tp); hit && tp > 0 {
		m.startPendingClose(ctx, p, "take_profit", price, 1, database.OperationTakeProfit)
		return
	}

	tierName, target, ratio := nextPendingTierFromLive(p.Tiers)
	if tierName == "" || ratio <= 0 || target <= 0 {
		return
	}
	if price, hit := priceForTierTrigger(side, quote, target); hit {
		m.startPendingClose(ctx, p, tierName, price, ratio, opForTier(tierName))
	}
}

func nextPendingTierFromLive(t database.LiveTierRecord) (string, float64, float64) {
	if !t.Tier1Done && t.Tier1 > 0 {
		return "tier1", t.Tier1, ratioOrDefault(t.Tier1Ratio, defaultTier1Ratio)
	}
	if !t.Tier2Done && t.Tier2 > 0 {
		return "tier2", t.Tier2, ratioOrDefault(t.Tier2Ratio, defaultTier2Ratio)
	}
	if !t.Tier3Done && t.Tier3 > 0 {
		return "tier3", t.Tier3, ratioOrDefault(t.Tier3Ratio, defaultTier3Ratio)
	}
	return "", 0, 0
}

func opForTier(name string) database.OperationType {
	switch strings.ToLower(name) {
	case "tier1":
		return database.OperationTier1
	case "tier2":
		return database.OperationTier2
	case "tier3":
		return database.OperationTier3
	default:
		return database.OperationFailed
	}
}

// startPendingClose 在命中 tier/tp/sl 时先更新本地仓位/tiers，再触发 freqtrade ForceExit。
func (m *Manager) startPendingClose(ctx context.Context, p database.LiveOrderWithTiers, kind string, price float64, ratio float64, op database.OperationType) {
	tradeID := p.Order.FreqtradeID
	symbol := strings.ToUpper(strings.TrimSpace(p.Order.Symbol))
	side := strings.ToLower(strings.TrimSpace(p.Order.Side))
	if tradeID == 0 || symbol == "" || side == "" {
		return
	}
	if m.hasPendingExit(tradeID) {
		return
	}

	currAmount := valOrZero(p.Order.Amount)
	if currAmount <= 0 {
		return
	}

	remainingRatio := p.Tiers.RemainingRatio
	if remainingRatio <= 0 {
		remainingRatio = 1
	}

	effectiveRatio := math.Min(math.Max(ratio, 0), remainingRatio)
	if strings.EqualFold(kind, "stop_loss") || strings.EqualFold(kind, "take_profit") {
		effectiveRatio = remainingRatio
	}
	if effectiveRatio <= 0 {
		return
	}

	now := time.Now()
	entry := valOrZero(p.Order.Price)
	closeQty := currAmount * math.Min(1, effectiveRatio/remainingRatio)
	expectedAmount := math.Max(0, currAmount-(currAmount*effectiveRatio/remainingRatio))
	m.addPendingExit(pendingExit{
		TradeID:        tradeID,
		Symbol:         symbol,
		Side:           side,
		Kind:           kind,
		TargetPrice:    price,
		Ratio:          effectiveRatio,
		PrevAmount:     currAmount,
		PrevClosed:     valOrZero(p.Order.ClosedAmount),
		ExpectedAmount: expectedAmount,
		RequestedAt:    now,
		Operation:      op,
		EntryPrice:     entry,
		Stake:          valOrZero(p.Order.StakeAmount),
		Leverage:       valOrZero(p.Order.Leverage),
	})
	m.appendOperation(ctx, tradeID, symbol, op, map[string]any{
		"event_type":       "CLOSING_" + strings.ToUpper(kind),
		"price":            price,
		"close_ratio":      effectiveRatio,
		"close_quantity":   closeQty,
		"expected_amount":  expectedAmount,
		"remaining_ratio":  p.Tiers.RemainingRatio,
		"side":             side,
		"stake":            valOrZero(p.Order.StakeAmount),
		"leverage":         valOrZero(p.Order.Leverage),
		"entry_price":      entry,
		"take_profit":      p.Tiers.TakeProfit,
		"stop_loss":        p.Tiers.StopLoss,
		"tier1/2/3_done":   fmt.Sprintf("%v/%v/%v", p.Tiers.Tier1Done, p.Tiers.Tier2Done, p.Tiers.Tier3Done),
		"tier1/2/3_target": fmt.Sprintf("%.4f/%.4f/%.4f", p.Tiers.Tier1, p.Tiers.Tier2, p.Tiers.Tier3),
	})

	payload := ForceExitPayload{TradeID: fmt.Sprintf("%d", tradeID)}
	if closeQty > 0 {
		payload.Amount = closeQty
	}
	if m.client != nil {
		if err := m.client.ForceExit(ctx, payload); err != nil {
			m.appendOperation(ctx, tradeID, symbol, database.OperationFailed, map[string]any{
				"event_type": strings.ToUpper(kind),
				"error":      err.Error(),
				"amount":     closeQty,
			})
			m.notify("自动平仓指令失败 ❌",
				fmt.Sprintf("交易ID: %d", tradeID),
				fmt.Sprintf("标的: %s", symbol),
				fmt.Sprintf("事件: %s", strings.ToUpper(kind)),
				fmt.Sprintf("错误: %v", err),
			)
			return
		}
	}
}

// autoPartial 逻辑改为 pending，由 startPendingClose 触发。
