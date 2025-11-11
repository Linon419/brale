package binance

import "time"

// Config 描述 Binance Source 运行所需的参数。
type Config struct {
	RESTBaseURL     string
	WSBaseURL       string
	RateLimitPerMin int
	WSBatchSize     int
	HTTPTimeout     time.Duration
}

func (c *Config) withDefaults() Config {
	out := *c
	if out.RESTBaseURL == "" {
		out.RESTBaseURL = "https://fapi.binance.com"
	}
	if out.WSBaseURL == "" {
		out.WSBaseURL = "wss://fstream.binance.com/stream"
	}
	if out.RateLimitPerMin <= 0 {
		out.RateLimitPerMin = 1200
	}
	if out.WSBatchSize <= 0 {
		out.WSBatchSize = 150
	}
	if out.HTTPTimeout <= 0 {
		out.HTTPTimeout = 15 * time.Second
	}
	return out
}
