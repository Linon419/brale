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

func (m *Manager) handlePriceTick(ctx context.Context, symbol string, quote TierPriceQuote) {
	if m == nil {
		return
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return
	}
	if quote.isEmpty() || quote.Last <= 0 {
		m.reportMissingPrice(symbol)
		return
	}
	m.clearMissingPrice(symbol)
	positions := m.positionsForSymbol(ctx, symbol)
	count := len(positions)
	if count == 0 {
		logger.Debugf("自动平仓检测: %s last=%.4f high=%.4f low=%.4f positions=%d", symbol, quote.Last, quote.High, quote.Low, count)
		return
	}
	logger.Debugf("自动平仓检测: %s last=%.4f high=%.4f low=%.4f positions=%d", symbol, quote.Last, quote.High, quote.Low, count)
	for _, p := range positions {
		if m.hasPendingExit(p.Order.FreqtradeID) {
			continue
		}
		if p.Order.Status != database.LiveOrderStatusOpen && p.Order.Status != database.LiveOrderStatusPartial {
			continue
		}
		if p.Tiers.IsPlaceholder || !hasCompleteTier(p.Tiers) {
			continue
		}
		side := strings.ToLower(strings.TrimSpace(p.Order.Side))
		if side == "" {
			continue
		}
		logger.Debugf("监控仓位 trade=%d side=%s status=%s entry=%.4f amount=%.6f remaining=%.2f%% tier_done=%v/%v/%v targets=%.4f/%.4f/%.4f stop=%.4f tp=%.4f",
			p.Order.FreqtradeID,
			side,
			statusText(p.Order.Status),
			valOrZero(p.Order.Price),
			valOrZero(p.Order.Amount),
			p.Tiers.RemainingRatio*100,
			p.Tiers.Tier1Done,
			p.Tiers.Tier2Done,
			p.Tiers.Tier3Done,
			p.Tiers.Tier1,
			p.Tiers.Tier2,
			p.Tiers.Tier3,
			p.Tiers.StopLoss,
			p.Tiers.TakeProfit,
		)
		lock := getPositionLock(p.Order.FreqtradeID)
		lock.Lock()
		m.evaluateOne(ctx, side, quote, p)
		lock.Unlock()
	}
}

func (m *Manager) positionsForSymbol(ctx context.Context, symbol string) []database.LiveOrderWithTiers {
	if m == nil {
		return nil
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil
	}
	positions := m.cachePositions()
	if len(positions) == 0 {
		positions = m.refreshCache(ctx)
	}
	if len(positions) == 0 {
		return nil
	}
	out := make([]database.LiveOrderWithTiers, 0, len(positions))
	for _, p := range positions {
		if strings.ToUpper(strings.TrimSpace(p.Order.Symbol)) == symbol {
			out = append(out, p)
		}
	}
	return out
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
	tradeID := p.Order.FreqtradeID

	logger.Debugf("自动平仓逐项检测 trade=%d side=%s last=%.4f high=%.4f low=%.4f stop=%.4f tp=%.4f tier_done=%v/%v/%v",
		tradeID, side, quote.Last, quote.High, quote.Low, stop, tp, p.Tiers.Tier1Done, p.Tiers.Tier2Done, p.Tiers.Tier3Done)

	m.logNearAutoCloseTargets(side, quote, p)

	if stop <= 0 {
		logger.Debugf("trade=%d 止损未设置，跳过 stop_loss 检测", tradeID)
	} else if price, hit := priceForStopLoss(side, quote, stop); hit {
		logger.Infof("trade=%d 止损命中 price=%.4f stop=%.4f", tradeID, price, stop)
		m.startPendingClose(ctx, p, "stop_loss", price, 1, database.OperationStopLoss, nil, true)
		return
	} else {
		logger.Debugf("trade=%d 止损未命中: 最新价 %.4f 未达到 stop %.4f", tradeID, quote.Last, stop)
	}

	if tp <= 0 {
		logger.Debugf("trade=%d 止盈未设置，跳过 take_profit 检测", tradeID)
	} else if price, hit := priceForTakeProfit(side, quote, tp); hit {
		logger.Infof("trade=%d 止盈命中 price=%.4f tp=%.4f", tradeID, price, tp)
		m.startPendingClose(ctx, p, "take_profit", price, 1, database.OperationTakeProfit, nil, true)
		return
	} else {
		logger.Debugf("trade=%d 止盈未命中: 最新价 %.4f 未达到 tp %.4f", tradeID, quote.Last, tp)
	}

	if result, ok := detectTierTrigger(tradeID, side, quote, p.Tiers); ok {
		m.startPendingClose(ctx, p, result.finalTier, result.triggerPrice, result.ratio, opForTier(result.finalTier), result.covered, false)
	} else {
		logger.Debugf("trade=%d 三段止盈未触发", tradeID)
	}
}

const nearTriggerThreshold = 0.000001

func (m *Manager) logNearAutoCloseTargets(side string, quote TierPriceQuote, p database.LiveOrderWithTiers) {
	tradeID := p.Order.FreqtradeID
	last := quote.Last
	check := func(label string, target float64, done bool) {
		if done {
			return
		}
		if ok, diff := nearTrigger(last, target); ok {
			logger.Infof("trade=%d %s 接近 target=%.4f last=%.4f 差距=%.6f%% side=%s", tradeID, label, target, last, diff*100, side)
		}
	}
	check("止损", p.Tiers.StopLoss, false)
	check("止盈", p.Tiers.TakeProfit, false)
	check("tier1", p.Tiers.Tier1, p.Tiers.Tier1Done)
	check("tier2", p.Tiers.Tier2, p.Tiers.Tier2Done)
	check("tier3", p.Tiers.Tier3, p.Tiers.Tier3Done)
}

func nearTrigger(last, target float64) (bool, float64) {
	if last <= 0 || target <= 0 {
		return false, 0
	}
	diff := math.Abs(last-target) / target
	if diff <= nearTriggerThreshold {
		return true, diff
	}
	return false, diff
}

type tierTriggerResult struct {
	finalTier    string
	triggerPrice float64
	ratio        float64
	covered      []string
}

func detectTierTrigger(tradeID int, side string, quote TierPriceQuote, t database.LiveTierRecord) (tierTriggerResult, bool) {
	type tierInfo struct {
		name   string
		target float64
		ratio  float64
		done   bool
	}
	candidates := []tierInfo{
		{"tier1", t.Tier1, ratioOrDefault(t.Tier1Ratio, defaultTier1Ratio), t.Tier1Done},
		{"tier2", t.Tier2, ratioOrDefault(t.Tier2Ratio, defaultTier2Ratio), t.Tier2Done},
		{"tier3", t.Tier3, ratioOrDefault(t.Tier3Ratio, defaultTier3Ratio), t.Tier3Done},
	}
	var covered []string
	var combinedRatio float64
	var triggerPrice float64
	for _, c := range candidates {
		logger.Debugf("trade=%d tier检测 %s target=%.4f ratio=%.2f%% done=%v last=%.4f", tradeID, c.name, c.target, c.ratio*100, c.done, quote.Last)
		if c.done {
			logger.Debugf("trade=%d %s 已完成，跳过", tradeID, c.name)
			continue
		}
		if c.target <= 0 || c.ratio <= 0 {
			logger.Debugf("trade=%d %s 目标或比例无效 target=%.4f ratio=%.4f", tradeID, c.name, c.target, c.ratio)
			continue
		}
		if price, hit := priceForTierTrigger(side, quote, c.target); hit {
			logger.Infof("trade=%d %s 命中 price=%.4f target=%.4f", tradeID, c.name, price, c.target)
			covered = append(covered, c.name)
			combinedRatio += c.ratio
			triggerPrice = price
			continue
		}
		logger.Debugf("trade=%d %s 未命中: side=%s last=%.4f target=%.4f", tradeID, c.name, side, quote.Last, c.target)
		// 价格未触发当前段位，后续更远的段位必然也未触发，直接中断。
		break
	}
	if len(covered) == 0 || combinedRatio <= 0 {
		return tierTriggerResult{}, false
	}
	if combinedRatio > 1 {
		combinedRatio = 1
	}
	return tierTriggerResult{
		finalTier:    covered[len(covered)-1],
		triggerPrice: triggerPrice,
		ratio:        combinedRatio,
		covered:      covered,
	}, true
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

// startPendingClose 在命中 tier/tp/sl 时先更新本地记录，再触发 freqtrade ForceExit。
func (m *Manager) startPendingClose(ctx context.Context, p database.LiveOrderWithTiers, kind string, price float64, ratio float64, op database.OperationType, covered []string, forceFull bool) {
	tradeID := p.Order.FreqtradeID
	symbol := strings.ToUpper(strings.TrimSpace(p.Order.Symbol))
	side := strings.ToLower(strings.TrimSpace(p.Order.Side))
	kindLower := strings.ToLower(strings.TrimSpace(kind))
	if tradeID == 0 || symbol == "" || side == "" {
		logger.Infof("startPendingClose 跳过: trade=%d symbol=%s side=%s kind=%s 参数缺失", tradeID, symbol, side, kind)
		return
	}
	if m.hasPendingExit(tradeID) {
		logger.Infof("startPendingClose 跳过: trade=%d kind=%s 已有 pending", tradeID, kind)
		return
	}

	currAmount := valOrZero(p.Order.Amount)
	if currAmount <= 0 {
		logger.Infof("startPendingClose 跳过: trade=%d kind=%s 当前数量<=0", tradeID, kind)
		return
	}

	initialAmount := valOrZero(p.Order.InitialAmount)
	if initialAmount <= 0 {
		initialAmount = currAmount
	}
	if initialAmount <= 0 {
		logger.Infof("startPendingClose 跳过: trade=%d kind=%s 初始数量<=0", tradeID, kind)
		return
	}
	effectiveRatio := math.Min(math.Max(ratio, 0), 1)
	if effectiveRatio <= 0 {
		logger.Infof("startPendingClose 跳过: trade=%d kind=%s ratio<=0 (%.4f)", tradeID, kind, ratio)
		return
	}
	closeQty := initialAmount * effectiveRatio
	if forceFull {
		closeQty = currAmount
	}
	if closeQty > currAmount {
		closeQty = currAmount
	}
	if closeQty <= 0 {
		logger.Infof("startPendingClose 跳过: trade=%d kind=%s closeQty<=0", tradeID, kind)
		return
	}
	if !forceFull && initialAmount > 0 {
		effectiveRatio = closeQty / initialAmount
	}

	now := time.Now()
	entry := valOrZero(p.Order.Price)
	targetAmount := math.Max(0, currAmount-closeQty)
	pe := pendingExit{
		TradeID:        tradeID,
		Symbol:         symbol,
		Side:           side,
		Kind:           kind,
		CoveredTiers:   append([]string(nil), covered...),
		TargetPrice:    price,
		Ratio:          effectiveRatio,
		PrevAmount:     currAmount,
		PrevClosed:     valOrZero(p.Order.ClosedAmount),
		InitialAmount:  initialAmount,
		TargetAmount:   targetAmount,
		ExpectedAmount: targetAmount,
		RequestedAt:    now,
		State:          pendingStateQueued,
		ForceFull:      forceFull,
		Operation:      op,
		EntryPrice:     entry,
		Stake:          valOrZero(p.Order.StakeAmount),
		Leverage:       valOrZero(p.Order.Leverage),
	}
	m.addPendingExit(pe)
	logger.Infof("自动平仓触发: trade=%d symbol=%s kind=%s trigger=%.4f qty=%.6f ratio=%.2f%% force_full=%v", tradeID, symbol, kindLower, price, closeQty, effectiveRatio*100, forceFull)

	switch kindLower {
	case "stop_loss":
		m.notify("止损触发 ⚠️",
			fmt.Sprintf("交易ID: %d | 标的: %s | 方向: %s", tradeID, symbol, side),
			fmt.Sprintf("触发点位: %.4f | 实际触发价: %.4f", p.Tiers.StopLoss, price),
			fmt.Sprintf("计划平仓: %.4f | 比例: %.2f%%", closeQty, effectiveRatio*100),
		)
	case "take_profit":
		target := p.Tiers.TakeProfit
		m.notify("止盈触发 ✅",
			fmt.Sprintf("交易ID: %d | 标的: %s | 方向: %s", tradeID, symbol, side),
			fmt.Sprintf("触发点位: %.4f | 实际触发价: %.4f", target, price),
			fmt.Sprintf("计划平仓: %.4f | 比例: %.2f%%", closeQty, effectiveRatio*100),
		)
	}

	if kindLower == "tier1" || kindLower == "tier2" || kindLower == "tier3" {
		target := price
		switch kindLower {
		case "tier1":
			if p.Tiers.Tier1 > 0 {
				target = p.Tiers.Tier1
			}
		case "tier2":
			if p.Tiers.Tier2 > 0 {
				target = p.Tiers.Tier2
			}
		case "tier3":
			if p.Tiers.Tier3 > 0 {
				target = p.Tiers.Tier3
			}
		}
		m.notify("分段止盈触发",
			fmt.Sprintf("交易ID: %d | 标的: %s | 方向: %s", tradeID, symbol, side),
			fmt.Sprintf("段位: %s | 目标价: %.4f | 触发价: %.4f", strings.ToUpper(kindLower), target, price),
		)
	}
	closingStatus := database.LiveOrderStatusClosingPartial
	if forceFull || closeQty >= currAmount {
		closingStatus = database.LiveOrderStatusClosingFull
	}
	m.updateOrderStatus(ctx, tradeID, symbol, side, closingStatus)
	remainingRatio := 0.0
	if initialAmount > 0 {
		remainingRatio = currAmount / initialAmount
	}
	details := map[string]any{
		"event_type":       "CLOSING_" + strings.ToUpper(kind),
		"price":            price,
		"close_ratio":      effectiveRatio,
		"close_quantity":   closeQty,
		"expected_amount":  targetAmount,
		"remaining_ratio":  remainingRatio,
		"side":             side,
		"stake":            valOrZero(p.Order.StakeAmount),
		"leverage":         valOrZero(p.Order.Leverage),
		"entry_price":      entry,
		"take_profit":      p.Tiers.TakeProfit,
		"stop_loss":        p.Tiers.StopLoss,
		"tier1/2/3_done":   fmt.Sprintf("%v/%v/%v", p.Tiers.Tier1Done, p.Tiers.Tier2Done, p.Tiers.Tier3Done),
		"tier1/2/3_target": fmt.Sprintf("%.4f/%.4f/%.4f", p.Tiers.Tier1, p.Tiers.Tier2, p.Tiers.Tier3),
	}
	if len(covered) > 0 {
		details["covered_tiers"] = strings.Join(covered, ",")
	}
	m.appendOperation(ctx, tradeID, symbol, op, details)

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
