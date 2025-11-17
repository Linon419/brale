package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"brale/internal/logger"
	"brale/internal/market"
)

const maxHistoryLimit = 1500

// Source 实现了 market.Source，负责 Binance REST/WS 接入。
type Source struct {
	cfg        Config
	httpClient *http.Client

	mu     sync.Mutex
	ws     *combinedStreamsClient
	cancel context.CancelFunc
	stats  market.SourceStats
}

func New(cfg Config) (*Source, error) {
	final := cfg.withDefaults()
	return &Source{
		cfg:        final,
		httpClient: &http.Client{Timeout: final.HTTPTimeout},
	}, nil
}

func (s *Source) FetchHistory(ctx context.Context, symbol, interval string, limit int) ([]market.Candle, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	interval = strings.ToLower(strings.TrimSpace(interval))
	if interval == "" {
		return nil, fmt.Errorf("interval is required")
	}
	url := fmt.Sprintf("%s/fapi/v1/klines?symbol=%s&interval=%s&limit=%d", s.cfg.RESTBaseURL, symbol, interval, limit)
	logger.Debugf("[binance] REST %s", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("binance history error: %s", resp.Status)
	}
	var raw [][]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]market.Candle, 0, len(raw))
	for _, k := range raw {
		if len(k) < 7 {
			continue
		}
		c := market.Candle{
			OpenTime:  toInt64(k[0]),
			CloseTime: toInt64(k[6]),
			Open:      toFloat(k[1]),
			High:      toFloat(k[2]),
			Low:       toFloat(k[3]),
			Close:     toFloat(k[4]),
			Volume:    toFloat(k[5]),
			Trades:    toInt64Safe(k, 8),
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Source) Subscribe(ctx context.Context, symbols, intervals []string, opts market.SubscribeOptions) (<-chan market.CandleEvent, error) {
	if len(symbols) == 0 || len(intervals) == 0 {
		return nil, fmt.Errorf("symbols and intervals are required for subscription")
	}
	batch := opts.BatchSize
	if batch <= 0 {
		batch = s.cfg.WSBatchSize
	}
	ws := newCombinedStreamsClient(s.cfg.WSBaseURL, batch)
	ws.SetCallbacks(opts.OnConnect, opts.OnDisconnect)
	if err := ws.Connect(); err != nil {
		return nil, err
	}

	subCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.ws != nil {
		s.ws.Close()
	}
	s.ws = ws
	s.cancel = cancel
	s.mu.Unlock()

	buffer := opts.Buffer
	if buffer <= 0 {
		buffer = 512
	}
	out := make(chan market.CandleEvent, buffer)
	var wg sync.WaitGroup

	nIntervals := normalizeIntervals(intervals)
	for _, sym := range symbols {
		upper := strings.ToUpper(strings.TrimSpace(sym))
		if upper == "" {
			continue
		}
		for _, iv := range nIntervals {
			stream := strings.ToLower(sym) + "@kline_" + iv
			sub := ws.AddSubscriber(stream, 200)
			wg.Add(1)
			go func(symbol, interval string, ch <-chan []byte) {
				defer wg.Done()
				s.forwardStream(subCtx, symbol, interval, ch, out)
			}(upper, iv, sub)
		}
	}
	for _, iv := range nIntervals {
		if err := ws.BatchSubscribeKlines(symbols, iv); err != nil {
			ws.Close()
			cancel()
			return nil, err
		}
	}

	go func() {
		<-subCtx.Done()
		ws.Close()
		wg.Wait()
		close(out)
	}()
	return out, nil
}

func (s *Source) forwardStream(ctx context.Context, symbol, interval string, stream <-chan []byte, out chan<- market.CandleEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-stream:
			if !ok {
				return
			}
			var ev klineEvent
			if err := json.Unmarshal(msg, &ev); err != nil {
				logger.Warnf("[binance] 解码 WS 帧失败: %v", err)
				continue
			}
			c := market.Candle{
				OpenTime:  ev.Kline.StartTime,
				CloseTime: ev.Kline.CloseTime,
				Open:      ev.Kline.OpenPrice.Float(),
				High:      ev.Kline.HighPrice.Float(),
				Low:       ev.Kline.LowPrice.Float(),
				Close:     ev.Kline.ClosePrice.Float(),
				Volume:    ev.Kline.Volume.Float(),
				Trades:    int64(ev.Kline.NumberOfTrades),
			}
			event := market.CandleEvent{Symbol: symbol, Interval: interval, Candle: c}
			select {
			case out <- event:
			default:
				logger.Warnf("[binance] 事件通道已满，丢弃 %s %s", symbol, interval)
			}
		}
	}
}

func (s *Source) Stats() market.SourceStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ws == nil {
		return market.SourceStats{}
	}
	return s.ws.Stats()
}

func (s *Source) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.ws != nil {
		s.ws.Close()
		s.ws = nil
	}
	return nil
}

func normalizeIntervals(intervals []string) []string {
	out := make([]string, 0, len(intervals))
	for _, iv := range intervals {
		trimmed := strings.ToLower(strings.TrimSpace(iv))
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	case float64:
		return t
	default:
		return 0
	}
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return int64(f)
	default:
		return 0
	}
}

func toInt64Safe(row []any, idx int) int64 {
	if idx < 0 || idx >= len(row) {
		return 0
	}
	return toInt64(row[idx])
}

type klineEvent struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`
	Kline     struct {
		StartTime           int64    `json:"t"`
		CloseTime           int64    `json:"T"`
		Symbol              string   `json:"s"`
		Interval            string   `json:"i"`
		OpenPrice           strOrNum `json:"o"`
		ClosePrice          strOrNum `json:"c"`
		HighPrice           strOrNum `json:"h"`
		LowPrice            strOrNum `json:"l"`
		Volume              strOrNum `json:"v"`
		NumberOfTrades      int      `json:"n"`
		IsFinal             bool     `json:"x"`
		QuoteVolume         strOrNum `json:"q"`
		TakerBuyBaseVolume  strOrNum `json:"V"`
		TakerBuyQuoteVolume strOrNum `json:"Q"`
		Ignore              strOrNum `json:"B"`
	} `json:"k"`
}

type strOrNum string

func (s *strOrNum) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = strOrNum(v)
		return nil
	}
	*s = strOrNum(string(b))
	return nil
}

func (s strOrNum) Float() float64 {
	f, _ := strconv.ParseFloat(string(s), 64)
	return f
}
