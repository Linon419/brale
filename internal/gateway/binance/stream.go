package binance

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"brale/internal/logger"
	"brale/internal/market"

	"github.com/gorilla/websocket"
)

type combinedStreamsClient struct {
	baseURL string

	mu          sync.RWMutex
	conn        *websocket.Conn
	subscribers map[string]chan []byte
	subscribed  map[string]bool
	pending     map[int64][]string

	batchSize int
	done      chan struct{}
	reconnect bool

	onConnect    func()
	onDisconnect func(error)

	stats market.SourceStats
}

func newCombinedStreamsClient(baseURL string, batchSize int) *combinedStreamsClient {
	if batchSize <= 0 {
		batchSize = 150
	}
	return &combinedStreamsClient{
		baseURL:     strings.TrimSpace(baseURL),
		batchSize:   batchSize,
		subscribers: make(map[string]chan []byte),
		subscribed:  make(map[string]bool),
		pending:     make(map[int64][]string),
		done:        make(chan struct{}),
		reconnect:   true,
	}
}

func (c *combinedStreamsClient) Connect() error {
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := d.Dial(c.baseURL, nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	go c.read()
	if c.onConnect != nil {
		c.onConnect()
	}
	return nil
}

func (c *combinedStreamsClient) Close() {
	c.mu.Lock()
	if !c.reconnect {
		c.mu.Unlock()
		return
	}
	c.reconnect = false
	conn := c.conn
	c.conn = nil
	for _, ch := range c.subscribers {
		close(ch)
	}
	c.subscribers = make(map[string]chan []byte)
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	close(c.done)
}

func (c *combinedStreamsClient) SetCallbacks(onConnect func(), onDisconnect func(error)) {
	c.onConnect = onConnect
	c.onDisconnect = onDisconnect
}

func (c *combinedStreamsClient) AddSubscriber(stream string, buf int) <-chan []byte {
	ch := make(chan []byte, buf)
	c.mu.Lock()
	c.subscribers[stream] = ch
	c.mu.Unlock()
	return ch
}

func (c *combinedStreamsClient) BatchSubscribeKlines(symbols []string, interval string) error {
	interval = strings.ToLower(strings.TrimSpace(interval))
	for i := 0; i < len(symbols); i += c.batchSize {
		end := i + c.batchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		params := make([]string, 0, end-i)
		for _, sym := range symbols[i:end] {
			params = append(params, strings.ToLower(sym)+"@kline_"+interval)
		}
		if err := c.subscribe(params); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (c *combinedStreamsClient) subscribe(params []string) error {
	if len(params) == 0 {
		return nil
	}
	id := time.Now().UnixNano()
	msg := map[string]any{"method": "SUBSCRIBE", "params": params, "id": id}
	for attempt := 1; attempt <= 3; attempt++ {
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()
		if conn == nil {
			return fmt.Errorf("ws not connected")
		}
		if err := conn.WriteJSON(msg); err != nil {
			if attempt == 3 {
				return err
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		c.mu.Lock()
		for _, p := range params {
			c.subscribed[p] = true
		}
		c.pending[id] = params
		c.mu.Unlock()
		return nil
	}
	return fmt.Errorf("subscribe failed after retries")
}

func (c *combinedStreamsClient) read() {
	for {
		select {
		case <-c.done:
			return
		default:
		}
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()
		if conn == nil {
			time.Sleep(time.Second)
			continue
		}
		_, message, err := conn.ReadMessage()
		if err != nil {
			if c.onDisconnect != nil {
				c.onDisconnect(err)
			}
			c.mu.Lock()
			c.stats.Reconnects++
			c.stats.LastError = err.Error()
			c.mu.Unlock()
			if !c.reconnect {
				return
			}
			time.Sleep(2 * time.Second)
			if err := c.Connect(); err != nil {
				logger.Warnf("[binance] WS 重连失败: %v", err)
				continue
			}
			c.replaySubscriptions()
			continue
		}
		if c.dispatchFrame(message) {
			continue
		}
	}
}

func (c *combinedStreamsClient) dispatchFrame(b []byte) bool {
	var frame struct {
		Stream string          `json:"stream"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &frame); err == nil && frame.Stream != "" {
		c.mu.RLock()
		ch := c.subscribers[frame.Stream]
		c.mu.RUnlock()
		if ch != nil {
			select {
			case ch <- frame.Data:
			default:
			}
		}
		return true
	}
	var ack struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(b, &ack); err == nil && ack.ID != 0 {
		c.mu.Lock()
		delete(c.pending, ack.ID)
		c.mu.Unlock()
		return true
	}
	var eframe struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		ID   int64  `json:"id"`
	}
	if err := json.Unmarshal(b, &eframe); err == nil && eframe.Code != 0 {
		c.mu.Lock()
		c.stats.SubscribeErrors++
		c.stats.LastError = eframe.Msg
		params := c.pending[eframe.ID]
		delete(c.pending, eframe.ID)
		c.mu.Unlock()
		if len(params) > 0 {
			_ = c.subscribe(params)
		}
		return true
	}
	return false
}

func (c *combinedStreamsClient) replaySubscriptions() {
	c.mu.RLock()
	streams := make([]string, 0, len(c.subscribed))
	for s := range c.subscribed {
		streams = append(streams, s)
	}
	c.mu.RUnlock()
	for i := 0; i < len(streams); i += c.batchSize {
		end := i + c.batchSize
		if end > len(streams) {
			end = len(streams)
		}
		if err := c.subscribe(streams[i:end]); err != nil {
			logger.Warnf("[binance] 重放订阅失败: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (c *combinedStreamsClient) Stats() market.SourceStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}
