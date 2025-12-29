package coins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"brale/internal/logger"
)

type SymbolProvider interface {
	List(ctx context.Context) ([]string, error)
	Name() string
}

func NormalizeSymbols(symbols []string) ([]string, error) {
	if len(symbols) == 0 {
		return nil, errors.New("symbol list is empty")
	}
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if !strings.HasSuffix(s, "USDT") {
			s += "USDT"
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, errors.New("symbol list is empty after normalization")
	}
	return out, nil
}

type DefaultSymbolProvider struct{ symbols []string }

func NewDefaultProvider(symbols []string) *DefaultSymbolProvider {
	return &DefaultSymbolProvider{symbols: symbols}
}

func (p *DefaultSymbolProvider) Name() string { return "default" }

func (p *DefaultSymbolProvider) List(_ context.Context) ([]string, error) {
	return NormalizeSymbols(p.symbols)
}

type HTTPSymbolProvider struct {
	URL    string
	Client *http.Client
}

func NewHTTPSymbolProvider(url string) *HTTPSymbolProvider {
	return &HTTPSymbolProvider{URL: url, Client: &http.Client{Timeout: 10 * time.Second}}
}

func (p *HTTPSymbolProvider) Name() string { return "http" }

func (p *HTTPSymbolProvider) List(ctx context.Context) ([]string, error) {
	if p.URL == "" {
		return nil, errors.New("symbol API URL not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching symbols: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var arr []string
	if err := json.Unmarshal(body, &arr); err == nil {
		return NormalizeSymbols(arr)
	}

	var obj struct {
		Symbols []string `json:"symbols"`
	}
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return NormalizeSymbols(obj.Symbols)
}

// OTCAPIResponse 定义 OTC API 的响应结构
type OTCAPIResponse struct {
	Success bool          `json:"success"`
	Date    string        `json:"date"`
	Count   int           `json:"count"`
	Items   []OTCCoinItem `json:"items"`
}

// OTCCoinItem 定义单个币种信息
type OTCCoinItem struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	OTCIndex      float64 `json:"otc_index"`
	ExplosionIndex float64 `json:"explosion_index"`
	PreviousExplosionIndex *float64 `json:"previous_explosion_index"`
	PeriodQuality string  `json:"period_quality"`
	Time          string  `json:"time"`
}

// OTCTargetItem stores normalized OTC API data for prompt rendering.
type OTCTargetItem struct {
	Symbol                 string
	Name                   string
	OTCIndex               float64
	ExplosionIndex         float64
	PreviousExplosionIndex *float64
	PeriodQuality          string
	Time                   string
}

// DynamicTargetsProvider 动态从 API 获取交易币种，支持自动刷新和 fallback
type DynamicTargetsProvider struct {
	apiURL         string
	quote          string
	timeout        time.Duration
	refreshSeconds int
	fallback       []string // 静态配置的备选列表
	override       bool     // true 时 API 结果覆盖 fallback

	mu          sync.RWMutex
	targets     []string
	otcItems    []OTCTargetItem
	lastFetched time.Time
	lastErr     error
}

// DynamicTargetsConfig 配置
type DynamicTargetsConfig struct {
	APIURL         string
	Quote          string
	TimeoutSeconds int
	RefreshSeconds int
	Fallback       []string
	Override       bool
}

// NewDynamicTargetsProvider 创建 DynamicTargetsProvider
func NewDynamicTargetsProvider(cfg DynamicTargetsConfig) *DynamicTargetsProvider {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	refreshSeconds := cfg.RefreshSeconds
	if refreshSeconds <= 0 {
		refreshSeconds = 3600 // 默认 1 小时刷新一次
	}
	quote := strings.ToUpper(strings.TrimSpace(cfg.Quote))
	if quote == "" {
		quote = "USDT"
	}

	fallback := normalizeSymbolsWithQuote(cfg.Fallback, quote)

	return &DynamicTargetsProvider{
		apiURL:         strings.TrimSpace(cfg.APIURL),
		quote:          quote,
		timeout:        timeout,
		refreshSeconds: refreshSeconds,
		fallback:       fallback,
		override:       cfg.Override,
		targets:        fallback, // 初始使用 fallback
	}
}

// Name 实现 SymbolProvider 接口
func (p *DynamicTargetsProvider) Name() string { return "dynamic" }

// List 实现 SymbolProvider 接口
func (p *DynamicTargetsProvider) List(ctx context.Context) ([]string, error) {
	// 尝试刷新（如果需要）
	_ = p.Refresh(ctx)
	return p.Targets(), nil
}

// Targets 返回当前的交易对列表
func (p *DynamicTargetsProvider) Targets() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, len(p.targets))
	copy(out, p.targets)
	return out
}

// OTCItems returns the last OTC API item snapshot (normalized with quote symbols).
func (p *DynamicTargetsProvider) OTCItems() []OTCTargetItem {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]OTCTargetItem, len(p.otcItems))
	copy(out, p.otcItems)
	return out
}

// Refresh 从 API 刷新币种列表
func (p *DynamicTargetsProvider) Refresh(ctx context.Context) error {
	if p.apiURL == "" {
		return nil // 没有配置 API，使用静态列表
	}

	p.mu.RLock()
	lastFetched := p.lastFetched
	p.mu.RUnlock()

	// 检查是否需要刷新
	if !lastFetched.IsZero() && time.Since(lastFetched) < time.Duration(p.refreshSeconds)*time.Second {
		return nil
	}

	symbols, items, err := p.fetchFromAPI(ctx)
	if err != nil {
		p.mu.Lock()
		p.lastErr = err
		p.mu.Unlock()
		logger.Warnf("DynamicTargetsProvider: API 获取失败，使用 fallback: %v", err)
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.override {
		// 覆盖模式：直接使用 API 结果
		p.targets = symbols
	} else {
		// 合并模式：API 结果 + fallback 去重
		p.targets = mergeAndDedup(symbols, p.fallback)
	}
	p.otcItems = items
	p.lastFetched = time.Now()
	p.lastErr = nil

	logger.Infof("DynamicTargetsProvider: 更新币种列表成功，共 %d 个: %v", len(p.targets), p.targets)
	return nil
}

// fetchFromAPI 从 OTC API 获取数据
func (p *DynamicTargetsProvider) fetchFromAPI(ctx context.Context) ([]string, []OTCTargetItem, error) {
	reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, p.apiURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create request failed: %w", err)
	}

	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response failed: %w", err)
	}

	var apiResp OTCAPIResponse
	if err := json.Unmarshal(body, &apiResp); err == nil && (len(apiResp.Items) > 0 || apiResp.Success) {
		if !apiResp.Success {
			return nil, nil, fmt.Errorf("API returned success=false")
		}
		symbols := make([]string, 0, len(apiResp.Items))
		items := make([]OTCTargetItem, 0, len(apiResp.Items))
		for _, item := range apiResp.Items {
			sym := strings.ToUpper(strings.TrimSpace(item.Symbol))
			if sym == "" {
				continue
			}
			full := fmt.Sprintf("%s/%s", sym, p.quote)
			symbols = append(symbols, full)
			items = append(items, OTCTargetItem{
				Symbol:                 full,
				Name:                   item.Name,
				OTCIndex:               item.OTCIndex,
				ExplosionIndex:         item.ExplosionIndex,
				PreviousExplosionIndex: item.PreviousExplosionIndex,
				PeriodQuality:          item.PeriodQuality,
				Time:                   item.Time,
			})
		}
		return symbols, items, nil
	}

	var arr []string
	if err := json.Unmarshal(body, &arr); err == nil {
		symbols, err := NormalizeSymbols(arr)
		return symbols, nil, err
	}

	var obj struct {
		Symbols []string `json:"symbols"`
	}
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, nil, fmt.Errorf("parsing response: %w", err)
	}
	symbols, err := NormalizeSymbols(obj.Symbols)
	return symbols, nil, err
}

// StartAutoRefresh 启动后台自动刷新
func (p *DynamicTargetsProvider) StartAutoRefresh(ctx context.Context) {
	if p.apiURL == "" {
		return
	}

	// 立即执行一次刷新
	if err := p.Refresh(ctx); err != nil {
		logger.Warnf("DynamicTargetsProvider: 初始刷新失败: %v", err)
	}

	// 后台定时刷新
	go func() {
		ticker := time.NewTicker(time.Duration(p.refreshSeconds) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := p.Refresh(ctx); err != nil {
					logger.Warnf("DynamicTargetsProvider: 定时刷新失败: %v", err)
				}
			}
		}
	}()
}

// normalizeSymbolsWithQuote 规范化币种列表
func normalizeSymbolsWithQuote(symbols []string, quote string) []string {
	if len(symbols) == 0 {
		return nil
	}
	out := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		s := strings.ToUpper(strings.TrimSpace(sym))
		if s == "" {
			continue
		}
		// 如果没有包含 /，添加 quote
		if !strings.Contains(s, "/") {
			s = fmt.Sprintf("%s/%s", s, quote)
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// mergeAndDedup 合并并去重
func mergeAndDedup(a, b []string) []string {
	seen := make(map[string]struct{})
	for _, s := range a {
		seen[s] = struct{}{}
	}
	for _, s := range b {
		seen[s] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
