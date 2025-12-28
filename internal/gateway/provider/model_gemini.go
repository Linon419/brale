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

type GeminiClient struct {
	BaseURL      string
	APIKey       string
	Model        string
	Timeout      time.Duration
	MaxRetries   int
	ExtraHeaders map[string]string
}

func (c *GeminiClient) Call(ctx context.Context, payload ChatPayload) (string, error) {
	ctx = ensureCtx(ctx)
	timeout := c.ensureTimeout()
	maxRetries := normalizeRetries(c.MaxRetries)
	url := c.generateContentURL()

	bodyBytes := buildGeminiBodyBytes(c.Model, payload)
	logger.LogLLMPayload(c.Model, string(bodyBytes))

	httpc := &http.Client{Timeout: timeout}
	return c.doGenerateContent(ctx, httpc, url, bodyBytes, maxRetries)
}

func (c *GeminiClient) ensureTimeout() time.Duration {
	if c.Timeout <= 0 {
		c.Timeout = 60 * time.Second
	}
	return c.Timeout
}

func (c *GeminiClient) generateContentURL() string {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta"
	}
	lower := strings.ToLower(base)
	if strings.Contains(lower, ":generatecontent") {
		return base
	}
	if strings.HasSuffix(lower, "/models") {
		return base + "/" + c.Model + ":generateContent"
	}
	if strings.Contains(lower, "/models/") {
		return base + ":generateContent"
	}
	if strings.HasSuffix(lower, "/v1beta") {
		return base + "/models/" + c.Model + ":generateContent"
	}
	return base + "/v1beta/models/" + c.Model + ":generateContent"
}

func buildGeminiBodyBytes(model string, payload ChatPayload) []byte {
	parts := buildGeminiParts(payload)
	body := map[string]any{
		"contents": []any{
			map[string]any{
				"role":  "user",
				"parts": parts,
			},
		},
		"generationConfig": map[string]any{
			"temperature":    0.4,
			"maxOutputTokens": geminiMaxTokens(payload.MaxTokens),
		},
	}
	if strings.TrimSpace(payload.System) != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []any{map[string]any{"text": payload.System}},
		}
	}
	if payload.ExpectJSON {
		cfg := body["generationConfig"].(map[string]any)
		cfg["responseMimeType"] = "application/json"
	}
	b, _ := json.Marshal(body)
	return b
}

func geminiMaxTokens(maxTokens int) int {
	if maxTokens <= 0 {
		return 4096
	}
	return maxTokens
}

func buildGeminiParts(payload ChatPayload) []map[string]any {
	parts := make([]map[string]any, 0, len(payload.Images)*2+1)
	parts = append(parts, map[string]any{"text": payload.User})
	for _, img := range payload.Images {
		if strings.TrimSpace(img.DataURI) == "" {
			continue
		}
		mediaType, data, ok := parseGeminiDataURI(img.DataURI)
		if !ok {
			logger.Warnf("[AI] Gemini: invalid image data uri, skipping")
			continue
		}
		parts = append(parts, map[string]any{
			"inlineData": map[string]any{
				"mimeType": mediaType,
				"data":     data,
			},
		})
		if desc := strings.TrimSpace(img.Description); desc != "" {
			parts = append(parts, map[string]any{"text": desc})
		}
	}
	return parts
}

func parseGeminiDataURI(raw string) (mediaType, data string, ok bool) {
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

func (c *GeminiClient) doGenerateContent(ctx context.Context, httpc *http.Client, url string, body []byte, maxRetries int) (string, error) {
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
			content, err := decodeGeminiContent(resp)
			if err != nil {
				lastErr = err
				break
			}
			return content, nil
		}

		msg := parseGeminiError(resp)
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

func decodeGeminiContent(resp *http.Response) (string, error) {
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			logger.Debugf("[AI] response body close failed: %v", cerr)
		}
	}()
	var r struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if len(r.Candidates) == 0 {
		return "", fmt.Errorf("empty candidates")
	}
	var b strings.Builder
	for _, part := range r.Candidates[0].Content.Parts {
		if strings.TrimSpace(part.Text) != "" {
			b.WriteString(part.Text)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("empty text content")
	}
	return out, nil
}

func parseGeminiError(resp *http.Response) string {
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

func (c *GeminiClient) headers() map[string]string {
	out := map[string]string{"Content-Type": "application/json"}
	if c.APIKey != "" && !headerKeyExistsGemini(c.ExtraHeaders, "x-api-key") {
		out["x-api-key"] = c.APIKey
	}
	for k, v := range c.ExtraHeaders {
		out[k] = v
	}
	return out
}

func (c *GeminiClient) headersForLog() map[string]string {
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

func headerKeyExistsGemini(headers map[string]string, key string) bool {
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

type GeminiModelProvider struct {
	id             string
	enabled        bool
	supportsVision bool
	expectJSON     bool
	client         interface {
		Call(ctx context.Context, payload ChatPayload) (string, error)
	}
}

func NewGeminiModelProvider(id string, enabled bool, supportsVision, expectJSON bool, client interface {
	Call(context.Context, ChatPayload) (string, error)
}) *GeminiModelProvider {
	return &GeminiModelProvider{
		id:             id,
		enabled:        enabled,
		supportsVision: supportsVision,
		expectJSON:     expectJSON,
		client:         client,
	}
}

func (p *GeminiModelProvider) ID() string           { return p.id }
func (p *GeminiModelProvider) Enabled() bool        { return p.enabled }
func (p *GeminiModelProvider) SupportsVision() bool { return p.supportsVision }
func (p *GeminiModelProvider) ExpectsJSON() bool    { return p.expectJSON }
func (p *GeminiModelProvider) Call(ctx context.Context, payload ChatPayload) (string, error) {
	return p.client.Call(ctx, payload)
}
