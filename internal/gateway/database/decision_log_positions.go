package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// UpsertLivePosition 更新或新增一条当前持仓记录（按 symbol+side 唯一）。
func (s *DecisionLogStore) UpsertLivePosition(ctx context.Context, pos LivePosition) error {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return fmt.Errorf("decision log store 未初始化")
	}
	sym := strings.ToUpper(strings.TrimSpace(pos.Symbol))
	if sym == "" {
		return fmt.Errorf("symbol 不能为空")
	}
	side := normalizeSide(pos.Side)
	if side == "" {
		return fmt.Errorf("side 不能为空")
	}
	now := time.Now().UnixMilli()
	opened := pos.OpenedAt
	if opened == 0 {
		opened = now
	}
	qty := pos.Quantity
	notional := pos.Notional
	res, err := db.ExecContext(ctx, `
        UPDATE live_positions
        SET entry_price=?, exit_price=NULL, quantity=?, notional=?, leverage=?, take_profit=?, stop_loss=?,
            pnl=0, status='open', closed_at=NULL, updated_at=?
        WHERE symbol=? AND side=? AND status='open'`,
		pos.Entry, qty, notional, nullIfZero(pos.Leverage), nullIfZero(pos.TakeProfit), nullIfZero(pos.StopLoss), now, sym, side)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		_, err = db.ExecContext(ctx, `
            INSERT INTO live_positions
                (symbol, side, entry_price, exit_price, quantity, notional, leverage, take_profit, stop_loss,
                 pnl, status, opened_at, closed_at, updated_at)
            VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, 0, 'open', ?, NULL, ?)`,
			sym, side, pos.Entry, qty, notional, nullIfZero(pos.Leverage), nullIfZero(pos.TakeProfit), nullIfZero(pos.StopLoss), opened, now)
		return err
	}
	return nil
}

// CloseLivePosition 将指定持仓标记为已关闭。
func (s *DecisionLogStore) CloseLivePosition(ctx context.Context, symbol, side string, exitPrice float64) error {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return fmt.Errorf("decision log store 未初始化")
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return fmt.Errorf("symbol 不能为空")
	}
	side = normalizeSide(side)
	if side == "" {
		return fmt.Errorf("side 不能为空")
	}
	now := time.Now().UnixMilli()
	_, err := db.ExecContext(ctx, `
        UPDATE live_positions
        SET exit_price=?, status='closed', closed_at=?, updated_at=?, notional=0, quantity=0
        WHERE symbol=? AND side=? AND status='open'`,
		nullIfZero(exitPrice), now, now, sym, side)
	return err
}

// ReduceLivePosition 根据 close_ratio 递减仓位，并在剩余极小时自动关闭。
func (s *DecisionLogStore) ReduceLivePosition(ctx context.Context, symbol, side string, ratio float64) error {
	ratio = clampRatio(ratio)
	if ratio <= 0 {
		return nil
	}
	pos, err := s.getOpenPosition(ctx, symbol, side)
	if err != nil || pos == nil {
		return err
	}
	base := pos.Notional
	if base == 0 {
		base = pos.Quantity
	}
	remain := base * (1 - ratio)
	if remain <= 1 { // 低于 1 USDT 直接视为平仓
		return s.CloseLivePosition(ctx, symbol, side, 0)
	}
	now := time.Now().UnixMilli()
	qty := pos.Quantity * (1 - ratio)
	if qty < 0 {
		qty = 0
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return fmt.Errorf("decision log store 未初始化")
	}
	_, err = db.ExecContext(ctx, `
        UPDATE live_positions
        SET notional=?, quantity=?, updated_at=?
        WHERE id=?`, remain, qty, now, pos.ID)
	return err
}

// UpdateLivePositionStops 更新当前持仓的止盈/止损，side 为空则对该 symbol 的所有持仓生效。
func (s *DecisionLogStore) UpdateLivePositionStops(ctx context.Context, symbol, side string, stopLoss, takeProfit float64) (bool, error) {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return false, fmt.Errorf("decision log store 未初始化")
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return false, fmt.Errorf("symbol 不能为空")
	}
	sets := make([]string, 0, 3)
	args := make([]interface{}, 0, 4)
	if stopLoss != 0 {
		sets = append(sets, "stop_loss=?")
		args = append(args, stopLoss)
	}
	if takeProfit != 0 {
		sets = append(sets, "take_profit=?")
		args = append(args, takeProfit)
	}
	if len(sets) == 0 {
		return false, nil
	}
	now := time.Now().UnixMilli()
	sets = append(sets, "updated_at=?")
	args = append(args, now)
	query := fmt.Sprintf("UPDATE live_positions SET %s WHERE symbol=? AND status='open'", strings.Join(sets, ", "))
	args = append(args, sym)
	if sSide := normalizeSide(side); sSide != "" {
		query += " AND side=?"
		args = append(args, sSide)
	}
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

// ListOpenPositions 返回所有未平仓记录。
func (s *DecisionLogStore) ListOpenPositions(ctx context.Context) ([]LivePosition, error) {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("decision log store 未初始化")
	}
	rows, err := db.QueryContext(ctx, `
        SELECT id, symbol, side, entry_price, exit_price, quantity, notional, leverage,
               take_profit, stop_loss, pnl, status, opened_at, closed_at, updated_at
        FROM live_positions
        WHERE status='open'
        ORDER BY opened_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LivePosition
	for rows.Next() {
		var pos LivePosition
		if err := rows.Scan(&pos.ID, &pos.Symbol, &pos.Side, &pos.Entry, &pos.Exit, &pos.Quantity,
			&pos.Notional, &pos.Leverage, &pos.TakeProfit, &pos.StopLoss, &pos.PnL, &pos.Status,
			&pos.OpenedAt, &pos.ClosedAt, &pos.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, pos)
	}
	return out, rows.Err()
}

func (s *DecisionLogStore) getOpenPosition(ctx context.Context, symbol, side string) (*LivePosition, error) {
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("decision log store 未初始化")
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return nil, nil
	}
	var query string
	var args []interface{}
	if sSide := normalizeSide(side); sSide != "" {
		query = `SELECT id, symbol, side, entry_price, exit_price, quantity, notional, leverage,
                 take_profit, stop_loss, pnl, status, opened_at, closed_at, updated_at
                 FROM live_positions WHERE symbol=? AND side=? AND status='open' ORDER BY id DESC LIMIT 1`
		args = []interface{}{sym, sSide}
	} else {
		query = `SELECT id, symbol, side, entry_price, exit_price, quantity, notional, leverage,
                 take_profit, stop_loss, pnl, status, opened_at, closed_at, updated_at
                 FROM live_positions WHERE symbol=? AND status='open' ORDER BY id DESC LIMIT 1`
		args = []interface{}{sym}
	}
	row := db.QueryRowContext(ctx, query, args...)
	var pos LivePosition
	if err := row.Scan(&pos.ID, &pos.Symbol, &pos.Side, &pos.Entry, &pos.Exit, &pos.Quantity,
		&pos.Notional, &pos.Leverage, &pos.TakeProfit, &pos.StopLoss, &pos.PnL, &pos.Status,
		&pos.OpenedAt, &pos.ClosedAt, &pos.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &pos, nil
}

func normalizeSide(side string) string {
	side = strings.ToLower(strings.TrimSpace(side))
	switch side {
	case "long", "short":
		return side
	default:
		return ""
	}
}

func clampRatio(ratio float64) float64 {
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}
