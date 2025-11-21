package decision

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	numberPattern   = regexp.MustCompile(`[-+]?\d*\.?\d+`)
	tierKeyReplacer = strings.NewReplacer("_", "", "-", "", " ", "")
)

// UnmarshalJSON 支持解析多种大小写/嵌套/字符串格式的 tiers 字段。
func (t *DecisionTiers) UnmarshalJSON(data []byte) error {
	type alias DecisionTiers
	if err := json.Unmarshal(data, (*alias)(t)); err != nil {
		return t.decodeTiersFallback(data)
	}
	t.fillMissingTiers(data)
	return nil
}

func (t *DecisionTiers) decodeTiersFallback(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.applyTierRaw(raw)
	return nil
}

func (t *DecisionTiers) fillMissingTiers(data []byte) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	t.applyTierRaw(raw)
}

func (t *DecisionTiers) applyTierRaw(raw map[string]any) {
	if len(raw) == 0 {
		return
	}
	norm := normalizeTierMap(raw)
	assignTierTarget(&t.Tier1Target, norm, "tier1target", "tier1tp", "tier1price", "tier1exit", "tier1takeprofit", "tier1", "t1")
	assignTierTarget(&t.Tier2Target, norm, "tier2target", "tier2tp", "tier2price", "tier2exit", "tier2takeprofit", "tier2", "t2")
	assignTierTarget(&t.Tier3Target, norm, "tier3target", "tier3tp", "tier3price", "tier3exit", "tier3takeprofit", "tier3", "t3")

	assignTierRatio(&t.Tier1Ratio, norm, "tier1ratio", "tier1portion", "tier1percent", "tier1pct", "tier1share", "tier1weight", "tier1")
	assignTierRatio(&t.Tier2Ratio, norm, "tier2ratio", "tier2portion", "tier2percent", "tier2pct", "tier2share", "tier2weight", "tier2")
	assignTierRatio(&t.Tier3Ratio, norm, "tier3ratio", "tier3portion", "tier3percent", "tier3pct", "tier3share", "tier3weight", "tier3")
}

func assignTierTarget(dst *float64, norm map[string]any, keys ...string) {
	if dst == nil || *dst > 0 {
		return
	}
	for _, key := range keys {
		if val, ok := norm[key]; ok {
			if target, ok := extractTierTarget(val); ok && target > 0 {
				*dst = target
				return
			}
		}
	}
}

func assignTierRatio(dst *float64, norm map[string]any, keys ...string) {
	if dst == nil || *dst > 0 {
		return
	}
	for _, key := range keys {
		if val, ok := norm[key]; ok {
			if ratio, ok := extractTierRatio(val); ok && ratio >= 0 {
				*dst = ratio
				return
			}
		}
	}
}

func extractTierTarget(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case map[string]any:
		inner := normalizeTierMap(v)
		for _, key := range []string{"target", "price", "value"} {
			if target, ok := convertSimpleFloat(inner[key], false); ok && target > 0 {
				return target, true
			}
		}
		return 0, false
	default:
		return convertSimpleFloat(v, false)
	}
}

func extractTierRatio(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case map[string]any:
		inner := normalizeTierMap(v)
		for _, key := range []string{"ratio", "portion", "percent", "share", "weight"} {
			if ratio, ok := convertSimpleFloat(inner[key], true); ok && ratio >= 0 {
				return ratio, true
			}
		}
		return 0, false
	default:
		return convertSimpleFloat(v, true)
	}
}

func convertSimpleFloat(val interface{}, treatPercent bool) (float64, bool) {
	switch v := val.(type) {
	case nil:
		return 0, false
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		return parseNumericString(v, treatPercent)
	default:
		return 0, false
	}
}

func parseNumericString(input string, treatPercent bool) (float64, bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return 0, false
	}
	lower := strings.ToLower(s)
	percentLike := strings.Contains(lower, "%") || strings.Contains(lower, "percent") || strings.Contains(lower, "pct")
	token := numberPattern.FindString(strings.ReplaceAll(s, ",", ""))
	if token == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(token, 64)
	if err != nil {
		return 0, false
	}
	if treatPercent && percentLike {
		f = f / 100
	}
	return f, true
}

func normalizeTierMap(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	norm := make(map[string]any, len(raw))
	for k, v := range raw {
		nk := normalizeTierKey(k)
		if nk == "" {
			continue
		}
		norm[nk] = v
	}
	return norm
}

func normalizeTierKey(key string) string {
	k := strings.TrimSpace(key)
	if k == "" {
		return ""
	}
	k = strings.ToLower(k)
	return tierKeyReplacer.Replace(k)
}
