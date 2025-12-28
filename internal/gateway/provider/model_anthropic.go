package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"brale/internal/logger"
)

type AnthropicClient struct {
	BaseURL      string
	APIKey       string
	Model        string
	Timeout      time.Duration
	MaxRetries   int
	ExtraHeaders map[string]string
}

func (c *AnthropicClient) Call(ctx context.Context, payload ChatPayload) (string, error) {
	ctx = ensureCtx(ctx)
	timeout := c.ensureTimeout()
	maxRetries := normalizeRetries(c.MaxRetries)
	url := c.messagesURL()

	bodyBytes := buildAnthropicBodyBytes(c.Model, payload)
	logger.LogLLMPayload(c.Model, string(bodyBytes))

	httpc := &http.Client{Timeout: timeout}
	return c.doMessages(ctx, httpc, url, bodyBytes, maxRetries)
}

func (c *AnthropicClient) ensureTimeout() time.Duration {
	if c.Timeout <= 0 {
		c.Timeout = 60 * time.Second
	}
	return c.Timeout
}

func (c *AnthropicClient) messagesURL() string {
	url := strings.TrimRight(c.BaseURL, "/")
	if url == "" {
		url = "https://api.anthropic.com/v1"
	}
	url = strings.TrimSuffix(url, "/messages")
	return url + "/messages"
}

func buildAnthropicBodyBytes(model string, payload ChatPayload) []byte {
	content := buildAnthropicContent(payload)
	msgs := []map[string]any{{
		"role":    "user",
		"content": content,
	}}
	maxTokens := payload.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	body := map[string]any{
		"model":       model,
		"messages":    msgs,
		"temperature": 0.4,
		"max_tokens":  maxTokens,
	}
	if strings.TrimSpace(payload.System) != "" {
		body["system"] = payload.System
	}
	b, _ := json.Marshal(body)
	return b
}

func buildAnthropicContent(payload ChatPayload) []map[string]any {
	blocks := make([]map[string]any, 0, len(payload.Images)*2+1)
	blocks = append(blocks, map[string]any{"type": "text", "text": payload.User})
	for _, img := range payload.Images {
		if strings.TrimSpace(img.DataURI) == "" {
			continue
		}
		mediaType, data, ok := parseDataURI(img.DataURI)
		if !ok {
			logger.Warnf("[AI] Anthropic: invalid image data uri, skipping")
			continue
		}
		blocks = append(blocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mediaType,
				"data":       data,
			},
		})
		if desc := strings.TrimSpace(img.Description); desc != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": desc})
		}
	}
	return blocks
}

func parseDataURI(raw string) (mediaType, data string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "data:") {
		return "", "", false
	}
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return "", "", false
	}
	meta := strings.TrimSpace(raw[len("data:"):comma])
	data = strings.TrimSpace(raw[comma+1:])
	if data == "" {
		return "", "", false
	}
	parts := strings.Split(meta, ";")
	if len(parts) == 0 {
		return "", "", false
	}
	mediaType = strings.TrimSpace(parts[0])
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	hasBase64 := false
	for _, part := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(part), "base64") {
			hasBase64 = true
			break
		}
	}
	if !hasBase64 {
		return "", "", false
	}
	return mediaType, data, true
}

func (c *AnthropicClient) doMessages(ctx context.Context, httpc *http.Client, url string, body []byte, maxRetries int) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt == 0 {
			logger.Debugf("[AI] 请求: POST %s headers=%v body=%s", url, redactHeaders(c.headersForLog()), string(body))
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		for k, v := range c.headers() {
			req.Header.Set(k, v)
		}
		resp, err := httpc.Do(req)
		if err != nil {
			lastErr = err
			break
		}

		if resp.StatusCode/100 == 2 {
			content, err := decodeAnthropicContent(resp)
			if err != nil {
				lastErr = err
				break
			}
			return content, nil
		}

		msg := parseAnthropicError(resp)
		lastErr = fmt.Errorf("status=%d: %s", resp.StatusCode, msg)
		if shouldRetry(resp.StatusCode) && attempt < maxRetries {
			wait := parseRetryAfter(resp.Header.Get("Retry-After"), attempt)
			time.Sleep(wait)
			continue
		}
		break
	}
	return "", lastErr
}

func decodeAnthropicContent(resp *http.Response) (string, error) {
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			logger.Debugf("[AI] response body close failed: %v", cerr)
		}
	}()
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if len(r.Content) == 0 {
		return "", fmt.Errorf("empty content")
	}
	var b strings.Builder
	for _, block := range r.Content {
		if block.Type == "text" && block.Text != "" {
			b.WriteString(block.Text)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("empty text content")
	}
	return out, nil
}

func parseAnthropicError(resp *http.Response) string {
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			logger.Debugf("[AI] response body close failed: %v", cerr)
		}
	}()
	var eresp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&eresp); err == nil && strings.TrimSpace(eresp.Error.Message) != "" {
		return eresp.Error.Message
	}
	return resp.Status
}

func (c *AnthropicClient) headers() map[string]string {
	out := map[string]string{"Content-Type": "application/json"}
	if c.APIKey != "" && !headerKeyExists(c.ExtraHeaders, "x-api-key") {
		out["x-api-key"] = c.APIKey
	}
	if !headerKeyExists(c.ExtraHeaders, "anthropic-version") {
		out["anthropic-version"] = "2023-06-01"
	}
	for k, v := range c.ExtraHeaders {
		out[k] = v
	}
	return out
}

func (c *AnthropicClient) headersForLog() map[string]string {
	out := map[string]string{}
	for k, v := range c.headers() {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "auth") || strings.Contains(lk, "key") || strings.Contains(lk, "token") {
			if len(v) > 4 {
				out[k] = "****" + v[len(v)-4:]
			} else {
				out[k] = "****"
			}
			continue
		}
		out[k] = v
	}
	return out
}

func headerKeyExists(headers map[string]string, key string) bool {
	if len(headers) == 0 {
		return false
	}
	key = strings.ToLower(strings.TrimSpace(key))
	for k := range headers {
		if strings.ToLower(strings.TrimSpace(k)) == key {
			return true
		}
	}
	return false
}

type AnthropicModelProvider struct {
	id             string
	enabled        bool
	supportsVision bool
	expectJSON     bool
	client         interface {
		Call(ctx context.Context, payload ChatPayload) (string, error)
	}
}

func NewAnthropicModelProvider(id string, enabled bool, supportsVision, expectJSON bool, client interface {
	Call(context.Context, ChatPayload) (string, error)
}) *AnthropicModelProvider {
	return &AnthropicModelProvider{
		id:             id,
		enabled:        enabled,
		supportsVision: supportsVision,
		expectJSON:     expectJSON,
		client:         client,
	}
}

func (p *AnthropicModelProvider) ID() string           { return p.id }
func (p *AnthropicModelProvider) Enabled() bool        { return p.enabled }
func (p *AnthropicModelProvider) SupportsVision() bool { return p.supportsVision }
func (p *AnthropicModelProvider) ExpectsJSON() bool    { return p.expectJSON }
func (p *AnthropicModelProvider) Call(ctx context.Context, payload ChatPayload) (string, error) {
	return p.client.Call(ctx, payload)
}
