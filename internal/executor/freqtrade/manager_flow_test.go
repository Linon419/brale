package freqtrade

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"

	brcfg "brale/internal/config"
	"brale/internal/decision"
	"brale/internal/gateway/database"
)

// memoryStore 以内存 map 伪造 LivePositionStore，方便验证字段写入。
type memoryStore struct {
	orders map[int]database.LiveOrderRecord
	tiers  map[int]database.LiveTierRecord
	mods   []database.TierModificationLog
	ops    []database.TradeOperationRecord
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		orders: make(map[int]database.LiveOrderRecord),
		tiers:  make(map[int]database.LiveTierRecord),
	}
}

func (s *memoryStore) cloneOrder(rec database.LiveOrderRecord) database.LiveOrderRecord {
	out := rec
	if rec.Amount != nil {
		val := *rec.Amount
		out.Amount = &val
	}
	if rec.InitialAmount != nil {
		val := *rec.InitialAmount
		out.InitialAmount = &val
	}
	if rec.StakeAmount != nil {
		val := *rec.StakeAmount
		out.StakeAmount = &val
	}
	if rec.Leverage != nil {
		val := *rec.Leverage
		out.Leverage = &val
	}
	if rec.PositionValue != nil {
		val := *rec.PositionValue
		out.PositionValue = &val
	}
	if rec.Price != nil {
		val := *rec.Price
		out.Price = &val
	}
	if rec.ClosedAmount != nil {
		val := *rec.ClosedAmount
		out.ClosedAmount = &val
	}
	if rec.PnLRatio != nil {
		val := *rec.PnLRatio
		out.PnLRatio = &val
	}
	if rec.PnLUSD != nil {
		val := *rec.PnLUSD
		out.PnLUSD = &val
	}
	if rec.CurrentPrice != nil {
		val := *rec.CurrentPrice
		out.CurrentPrice = &val
	}
	if rec.CurrentProfitRatio != nil {
		val := *rec.CurrentProfitRatio
		out.CurrentProfitRatio = &val
	}
	if rec.CurrentProfitAbs != nil {
		val := *rec.CurrentProfitAbs
		out.CurrentProfitAbs = &val
	}
	if rec.UnrealizedPnLRatio != nil {
		val := *rec.UnrealizedPnLRatio
		out.UnrealizedPnLRatio = &val
	}
	if rec.UnrealizedPnLUSD != nil {
		val := *rec.UnrealizedPnLUSD
		out.UnrealizedPnLUSD = &val
	}
	if rec.RealizedPnLRatio != nil {
		val := *rec.RealizedPnLRatio
		out.RealizedPnLRatio = &val
	}
	if rec.RealizedPnLUSD != nil {
		val := *rec.RealizedPnLUSD
		out.RealizedPnLUSD = &val
	}
	if rec.StartTime != nil {
		val := *rec.StartTime
		out.StartTime = &val
	}
	if rec.EndTime != nil {
		val := *rec.EndTime
		out.EndTime = &val
	}
	if rec.LastStatusSync != nil {
		val := *rec.LastStatusSync
		out.LastStatusSync = &val
	}
	if rec.IsSimulated != nil {
		val := *rec.IsSimulated
		out.IsSimulated = &val
	}
	return out
}

func (s *memoryStore) cloneTier(rec database.LiveTierRecord) database.LiveTierRecord {
	return rec
}

func (s *memoryStore) UpsertLiveOrder(_ context.Context, rec database.LiveOrderRecord) error {
	s.orders[rec.FreqtradeID] = s.cloneOrder(rec)
	return nil
}

func (s *memoryStore) UpdateOrderStatus(_ context.Context, tradeID int, status database.LiveOrderStatus) error {
	ord, ok := s.orders[tradeID]
	if !ok {
		return sql.ErrNoRows
	}
	ord.Status = status
	ord.UpdatedAt = time.Now()
	s.orders[tradeID] = ord
	return nil
}

func (s *memoryStore) UpsertLiveTiers(_ context.Context, rec database.LiveTierRecord) error {
	s.tiers[rec.FreqtradeID] = s.cloneTier(rec)
	return nil
}

func (s *memoryStore) SavePosition(ctx context.Context, order database.LiveOrderRecord, tier database.LiveTierRecord) error {
	if err := s.UpsertLiveOrder(ctx, order); err != nil {
		return err
	}
	return s.UpsertLiveTiers(ctx, tier)
}

func (s *memoryStore) InsertTierModification(_ context.Context, log database.TierModificationLog) error {
	s.mods = append(s.mods, log)
	return nil
}

func (s *memoryStore) AppendTradeOperation(_ context.Context, op database.TradeOperationRecord) error {
	s.ops = append(s.ops, op)
	return nil
}

func (s *memoryStore) ListTierModifications(_ context.Context, tradeID int, _ int) ([]database.TierModificationLog, error) {
	out := make([]database.TierModificationLog, 0)
	for _, log := range s.mods {
		if log.FreqtradeID == tradeID {
			out = append(out, log)
		}
	}
	return out, nil
}

func (s *memoryStore) ListTradeOperations(_ context.Context, tradeID int, _ int) ([]database.TradeOperationRecord, error) {
	out := make([]database.TradeOperationRecord, 0)
	for _, op := range s.ops {
		if op.FreqtradeID == tradeID {
			out = append(out, op)
		}
	}
	return out, nil
}

func (s *memoryStore) GetLivePosition(_ context.Context, tradeID int) (database.LiveOrderRecord, database.LiveTierRecord, bool, error) {
	ord, ok := s.orders[tradeID]
	if !ok {
		return database.LiveOrderRecord{}, database.LiveTierRecord{}, false, nil
	}
	tier := s.tiers[tradeID]
	return s.cloneOrder(ord), s.cloneTier(tier), true, nil
}

func (s *memoryStore) ListActivePositions(_ context.Context, _ int) ([]database.LiveOrderWithTiers, error) {
	var out []database.LiveOrderWithTiers
	for id, ord := range s.orders {
		if ord.Status == database.LiveOrderStatusClosed {
			continue
		}
		out = append(out, database.LiveOrderWithTiers{Order: ord, Tiers: s.tiers[id]})
	}
	return out, nil
}

func (s *memoryStore) ListRecentPositions(ctx context.Context, limit int) ([]database.LiveOrderWithTiers, error) {
	return s.ListActivePositions(ctx, limit)
}

func (s *memoryStore) ListRecentPositionsPaged(ctx context.Context, _ string, limit int, _ int) ([]database.LiveOrderWithTiers, error) {
	return s.ListRecentPositions(ctx, limit)
}

func (s *memoryStore) CountRecentPositions(_ context.Context, _ string) (int, error) {
	return len(s.orders), nil
}

func (s *memoryStore) AddOrderPnLColumns() error { return nil }

type testNotifier struct {
	messages []string
}

func (n *testNotifier) SendText(text string) error {
	n.messages = append(n.messages, text)
	return nil
}

// newTestManager 构造注入内存存储的 Manager，方便复用。
func newTestManager(store *memoryStore) *Manager {
	cfg := brcfg.FreqtradeConfig{MinStopDistancePct: 0}
	m := &Manager{
		cfg:              cfg,
		posStore:         store,
		posRepo:          NewPositionRepo(store),
		notifier:         &testNotifier{},
		traceByKey:       make(map[string]int),
		traceByID:        make(map[int]string),
		pendingDec:       make(map[string]decision.Decision),
		tradeDec:         make(map[int]decision.Decision),
		pendingSymbolDec: make(map[string][]queuedDecision),
		positions:        make(map[int]Position),
		posCache:         make(map[int]database.LiveOrderWithTiers),
		pendingExits:     make(map[int]*pendingExit),
		locker:           positionLocker,
	}
	return m
}

func floatPtr(v float64) *float64 { return &v }

// seedPosition 用于插入统一的多单仓位，把流程起点固定下来。
func seedPosition(t *testing.T, m *Manager, store *memoryStore, tradeID int, amount float64) {
	t.Helper()
	now := time.Now()
	order := database.LiveOrderRecord{
		FreqtradeID:   tradeID,
		Symbol:        "ETHUSDT",
		Side:          "long",
		Amount:        floatPtr(amount),
		InitialAmount: floatPtr(amount),
		StakeAmount:   floatPtr(1000),
		Leverage:      floatPtr(5),
		PositionValue: floatPtr(5000),
		Price:         floatPtr(2840.64),
		ClosedAmount:  floatPtr(0),
		Status:        database.LiveOrderStatusOpen,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	order.StartTime = &now
	tier := database.LiveTierRecord{
		FreqtradeID: tradeID,
		Symbol:      "ETHUSDT",
		TakeProfit:  2900,
		StopLoss:    2821,
		Tier1:       2850,
		Tier1Ratio:  0.5,
		Tier2:       2875,
		Tier2Ratio:  0.3,
		Tier3:       2900,
		Tier3Ratio:  0.2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.SavePosition(context.Background(), order, tier); err != nil {
		t.Fatalf("保存初始仓位失败: %v", err)
	}
	m.updateCacheOrderTiers(order, tier)
	m.positions[tradeID] = Position{
		TradeID:    tradeID,
		Symbol:     "ETHUSDT",
		Side:       "long",
		Amount:     amount,
		Stake:      1000,
		Leverage:   5,
		EntryPrice: 2840.64,
		OpenedAt:   now,
	}
}

func exitFillMessage(tradeID int, qty, price float64) WebhookMessage {
	return WebhookMessage{
		Type:        "exit_fill",
		TradeID:     numericInt(tradeID),
		Pair:        "ETH/USDT:USDT",
		Direction:   "long",
		Amount:      numericFloat(qty),
		CloseRate:   numericFloat(price),
		StakeAmount: numericFloat(1000),
		ExitReason:  "force_exit",
	}
}

// TestStopLossFlow 覆盖“实时价格触及止损→状态改为 closing_full→webhook 回写 closed”的完整链路。
func TestStopLossFlow(t *testing.T) {
	store := newMemoryStore()
	mgr := newTestManager(store)
	seedPosition(t, mgr, store, 1, 1)
	quote := TierPriceQuote{Last: 2819.5, High: 2819.6, Low: 2819.4}
	mgr.handlePriceTick(context.Background(), "ETHUSDT", quote)
	ord := store.orders[1]
	if ord.Status != database.LiveOrderStatusClosingFull {
		t.Fatalf("止损触发后状态应为 closing_full, 实际=%v", ord.Status)
	}
	msg := WebhookMessage{
		Type:        "exit_fill",
		TradeID:     numericInt(1),
		Pair:        "ETH/USDT:USDT",
		Direction:   "long",
		Amount:      numericFloat(1),
		CloseRate:   numericFloat(2819.5),
		StakeAmount: numericFloat(1000),
		ExitReason:  "force_exit",
	}
	mgr.handleExit(context.Background(), msg, "exit_fill")
	updated := store.orders[1]
	if updated.Status != database.LiveOrderStatusClosed {
		t.Fatalf("止损完成后应为 closed, 实际=%v", updated.Status)
	}
	if valOrZero(updated.Amount) != 0 {
		t.Fatalf("平仓后剩余仓位应为 0, 实际=%.4f", valOrZero(updated.Amount))
	}
	tier := store.tiers[1]
	if !tier.Tier1Done || !tier.Tier2Done || !tier.Tier3Done {
		t.Fatalf("全量止损应标记三段完成, 实际=%+v", tier)
	}
	if tier.RemainingRatio != 0 {
		t.Fatalf("全量止损 remaining_ratio 应为 0, 实际=%.4f", tier.RemainingRatio)
	}
}

// TestTierPartialFlow 演示 tier1 命中后只平 50%，并检查 tier1_done 与 remaining_ratio。
func TestTierPartialFlow(t *testing.T) {
	store := newMemoryStore()
	mgr := newTestManager(store)
	seedPosition(t, mgr, store, 2, 1)
	quote := TierPriceQuote{Last: 2851, High: 2855, Low: 2850}
	mgr.handlePriceTick(context.Background(), "ETHUSDT", quote)
	ord := store.orders[2]
	if ord.Status != database.LiveOrderStatusClosingPartial {
		t.Fatalf("tier1 命中后应为 closing_partial, 实际=%v", ord.Status)
	}
	msg := WebhookMessage{
		Type:        "exit_fill",
		TradeID:     numericInt(2),
		Pair:        "ETH/USDT:USDT",
		Direction:   "long",
		Amount:      numericFloat(0.5),
		CloseRate:   numericFloat(2851),
		StakeAmount: numericFloat(1000),
		ExitReason:  "force_exit",
	}
	mgr.handleExit(context.Background(), msg, "exit_fill")
	updated := store.orders[2]
	if updated.Status != database.LiveOrderStatusPartial {
		t.Fatalf("分段平仓后应回到 partial, 实际=%v", updated.Status)
	}
	if math.Abs(valOrZero(updated.Amount)-0.5) > 1e-6 {
		t.Fatalf("剩余仓位应为 0.5, 实际=%.4f", valOrZero(updated.Amount))
	}
	tier := store.tiers[2]
	if !tier.Tier1Done || tier.Tier2Done || tier.Tier3Done {
		t.Fatalf("tier 完成标记异常: %+v", tier)
	}
	if math.Abs(tier.RemainingRatio-0.5) > 1e-6 {
		t.Fatalf("RemainingRatio 应为 0.5, 实际=%.4f", tier.RemainingRatio)
	}
}

// TestTierCombineFlow 检查一次价格冲破 tier1+tier2 时，合并下单比例与字段更新是否正确。
func TestTierCombineFlow(t *testing.T) {
	store := newMemoryStore()
	mgr := newTestManager(store)
	seedPosition(t, mgr, store, 3, 1)
	quote := TierPriceQuote{Last: 2876, High: 2878, Low: 2851}
	mgr.handlePriceTick(context.Background(), "ETHUSDT", quote)
	if store.orders[3].Status != database.LiveOrderStatusClosingPartial {
		t.Fatalf("tier1+tier2 命中后应为 closing_partial, 实际=%v", store.orders[3].Status)
	}
	mgr.handleExit(context.Background(), exitFillMessage(3, 0.8, 2876), "exit_fill")
	ord := store.orders[3]
	if ord.Status != database.LiveOrderStatusPartial {
		t.Fatalf("回写后应维持 partial, 实际=%v", ord.Status)
	}
	if math.Abs(valOrZero(ord.Amount)-0.2) > 1e-6 {
		t.Fatalf("剩余仓位应为 0.2, 实际=%.4f", valOrZero(ord.Amount))
	}
	tier := store.tiers[3]
	if !tier.Tier1Done || !tier.Tier2Done || tier.Tier3Done {
		t.Fatalf("tier 完成标记应为 1/1/0, 实际=%+v", tier)
	}
	if math.Abs(tier.RemainingRatio-0.2) > 1e-6 {
		t.Fatalf("remaining_ratio 应为 0.2, 实际=%.4f", tier.RemainingRatio)
	}
}

// TestTakeProfitFlow 确保到达 take profit 时会强制全量平仓并回写 closed。
func TestTakeProfitFlow(t *testing.T) {
	store := newMemoryStore()
	mgr := newTestManager(store)
	seedPosition(t, mgr, store, 4, 1)
	mgr.handlePriceTick(context.Background(), "ETHUSDT", TierPriceQuote{Last: 2905, High: 2905, Low: 2899})
	if store.orders[4].Status != database.LiveOrderStatusClosingFull {
		t.Fatalf("take profit 命中后应为 closing_full, 实际=%v", store.orders[4].Status)
	}
	mgr.handleExit(context.Background(), exitFillMessage(4, 1, 2905), "exit_fill")
	ord := store.orders[4]
	if ord.Status != database.LiveOrderStatusClosed {
		t.Fatalf("止盈完成后应为 closed, 实际=%v", ord.Status)
	}
	if valOrZero(ord.Amount) != 0 {
		t.Fatalf("止盈后剩余数量应为 0, 实际=%.4f", valOrZero(ord.Amount))
	}
	if store.tiers[4].RemainingRatio != 0 {
		t.Fatalf("止盈 remaining_ratio 应为 0, 实际=%.4f", store.tiers[4].RemainingRatio)
	}
}
