package ai

import (
    "bytes"
    "encoding/json"
    "fmt"
    "strings"
)

// PrettyJSON 尝试对 JSON 文本进行缩进美化；失败则返回原文
func PrettyJSON(raw string) string {
    raw = strings.TrimSpace(raw)
    if raw == "" { return raw }
    var v any
    if err := json.Unmarshal([]byte(raw), &v); err != nil {
        return raw
    }
    b, err := json.MarshalIndent(v, "", "  ")
    if err != nil { return raw }
    return string(b)
}

// TrimTo 限制字符串长度，超长则追加省略号
func TrimTo(s string, max int) string {
    if max <= 0 { return s }
    if len(s) <= max { return s }
    var buf bytes.Buffer
    buf.WriteString(s[:max])
    buf.WriteString("...")
    return buf.String()
}

// ParseDecisions 将 JSON 数组反序列化为 Decision 列表
func ParseDecisions(raw string) ([]Decision, error) {
    var ds []Decision
    if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &ds); err != nil {
        return nil, err
    }
    return ds, nil
}

// FormatDecisionsTable 将决策渲染为简易表格
func FormatDecisionsTable(ds []Decision) string {
    if len(ds) == 0 { return "" }
    // 头
    var b strings.Builder
    b.WriteString("symbol        | action       | conf | reasoning\n")
    b.WriteString("--------------+--------------+------+------------------------------\n")
    for _, d := range ds {
        sym := pad(d.Symbol, 12)
        act := pad(d.Action, 12)
        conf := fmt.Sprintf("%4d", d.Confidence)
        reason := strings.ReplaceAll(d.Reasoning, "\n", " ")
        if len(reason) > 80 { reason = reason[:80] + "..." }
        b.WriteString(fmt.Sprintf("%s | %s | %s | %s\n", sym, act, conf, reason))
    }
    return b.String()
}

func pad(s string, n int) string {
    if len(s) >= n { return s[:n] }
    return s + strings.Repeat(" ", n-len(s))
}
