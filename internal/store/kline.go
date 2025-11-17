package store

import (
	"context"
	"errors"
	"sync"

	"brale/internal/market"
)

// KlineStore 抽象：读写 symbol+interval 的序列
type KlineStore interface {
	Put(ctx context.Context, symbol, interval string, ks []market.Candle, max int) error
	Get(ctx context.Context, symbol, interval string) ([]market.Candle, error)
}

// SnapshotExporter 导出固定窗口 K 线的抽象。
type SnapshotExporter interface {
	Export(ctx context.Context, symbol, interval string, limit int) ([]market.Candle, error)
}

// MemoryKlineStore 内存实现
type MemoryKlineStore struct {
	mu   sync.RWMutex
	data map[string][]market.Candle
}

func NewMemoryKlineStore() *MemoryKlineStore {
	return &MemoryKlineStore{data: make(map[string][]market.Candle)}
}
func key(symbol, interval string) string { return symbol + "@" + interval }

// Put 追加并裁剪
func (s *MemoryKlineStore) Put(ctx context.Context, symbol, interval string, ks []market.Candle, max int) error {
	if symbol == "" || interval == "" {
		return errors.New("symbol/interval 不能为空")
	}
	if len(ks) == 0 {
		return nil
	}
	if max <= 0 {
		max = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(symbol, interval)
	cur := s.data[k]
	for _, candle := range ks {
		n := len(cur)
		if n > 0 && cur[n-1].OpenTime == candle.OpenTime {
			// 同一根 K 线的增量更新，覆盖末尾而非重复追加。
			cur[n-1] = candle
			continue
		}
		cur = append(cur, candle)
	}
	if len(cur) > max {
		cur = cur[len(cur)-max:]
	}
	s.data[k] = cur
	return nil
}

// Set 全量替换指定 symbol+interval 的序列
func (s *MemoryKlineStore) Set(ctx context.Context, symbol, interval string, ks []market.Candle) error {
	if symbol == "" || interval == "" {
		return errors.New("symbol/interval 不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(symbol, interval)
	dst := make([]market.Candle, len(ks))
	copy(dst, ks)
	s.data[k] = dst
	return nil
}

// Get 返回拷贝
func (s *MemoryKlineStore) Get(ctx context.Context, symbol, interval string) ([]market.Candle, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cur := s.data[key(symbol, interval)]
	out := make([]market.Candle, len(cur))
	copy(out, cur)
	return out, nil
}

// Export 返回最近 limit 根 K 线（按时间升序）
func (s *MemoryKlineStore) Export(ctx context.Context, symbol, interval string, limit int) ([]market.Candle, error) {
	if symbol == "" || interval == "" {
		return nil, errors.New("symbol/interval 不能为空")
	}
	if limit <= 0 {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	cur := s.data[key(symbol, interval)]
	if len(cur) == 0 {
		return nil, nil
	}
	if limit > len(cur) {
		limit = len(cur)
	}
	out := make([]market.Candle, limit)
	copy(out, cur[len(cur)-limit:])
	return out, nil
}
