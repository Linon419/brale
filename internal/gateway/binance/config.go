package binance

import "time"

// Config 描述 Binance Source 运行所需的参数。
type Config struct {
	RESTBaseURL string
	HTTPTimeout time.Duration
}

func (c *Config) withDefaults() Config {
	out := *c
	if out.RESTBaseURL == "" {
		out.RESTBaseURL = "https://fapi.binance.com"
	}
	if out.HTTPTimeout <= 0 {
		out.HTTPTimeout = 15 * time.Second
	}
	return out
}
