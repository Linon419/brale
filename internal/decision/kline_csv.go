package decision

import (
	"math"
	"strconv"
	"strings"
	"time"

	"brale/internal/market"
)

// CandleCSVOptions 控制 CSV 数据行的时间格式与精度。
type CandleCSVOptions struct {
	DateOnly       bool
	Location       *time.Location
	PricePrecision int
}

// CandleCSVBlockOptions 控制完整 block 的渲染。
type CandleCSVBlockOptions struct {
	CandleCSVOptions
	StageTitle string
	BlockTag   string
}

const (
	// PrecisionAuto 根据 K 线价格区间自动决定精度。
	PrecisionAuto = math.MinInt32
	// PrecisionRaw 表示保留原始精度（等价于 strconv.FormatFloat(..., -1, 64)）
	PrecisionRaw = -1
)

// BuildCandleCSV 生成 CSV 数据，首行包含列头。
func BuildCandleCSV(candles []market.Candle, opts CandleCSVOptions) string {
	if len(candles) == 0 {
		return ""
	}
	loc := opts.Location
	if loc == nil {
		loc = time.UTC
	}
	precision := opts.PricePrecision
	if precision == PrecisionAuto {
		precision = autoPrecisionFromCandles(candles)
	}
	header := "Time"
	if opts.DateOnly {
		header = "Date"
	}
	var b strings.Builder
	b.WriteString(header + ",O,H,L,C,V,Trades\n")
	for _, c := range candles {
		ts := time.UnixMilli(c.CloseTime).In(loc)
		label := ts.Format("01-02 15:04")
		if opts.DateOnly {
			label = ts.Format("06-01-02")
		}
		b.WriteString(label)
		b.WriteByte(',')
		b.WriteString(formatPrice(c.Open, precision))
		b.WriteByte(',')
		b.WriteString(formatPrice(c.High, precision))
		b.WriteByte(',')
		b.WriteString(formatPrice(c.Low, precision))
		b.WriteByte(',')
		b.WriteString(formatPrice(c.Close, precision))
		b.WriteByte(',')
		b.WriteString(formatPlainFloat(c.Volume))
		b.WriteByte(',')
		b.WriteString(strconv.FormatInt(c.Trades, 10))
		b.WriteByte('\n')
	}
	return b.String()
}

// RenderCandleCSVBlock 输出带 `## title` 与 `[TAG_*]` 包裹的 block。
func RenderCandleCSVBlock(candles []market.Candle, opts CandleCSVBlockOptions) string {
	data := BuildCandleCSV(candles, opts.CandleCSVOptions)
	if data == "" {
		return ""
	}
	tag := strings.TrimSpace(opts.BlockTag)
	if tag == "" {
		tag = "DATA"
	}
	tag = strings.ToUpper(tag)
	var b strings.Builder
	if title := strings.TrimSpace(opts.StageTitle); title != "" {
		b.WriteString("## " + title + "\n")
	}
	b.WriteString("[" + tag + "_START]\n")
	b.WriteString(data)
	if !strings.HasSuffix(data, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("[" + tag + "_END]\n")
	return b.String()
}

func autoPrecisionFromCandles(candles []market.Candle) int {
	maxVal := 0.0
	for _, c := range candles {
		for _, v := range []float64{c.Open, c.High, c.Low, c.Close} {
			abs := math.Abs(v)
			if abs > maxVal {
				maxVal = abs
			}
		}
	}
	switch {
	case maxVal >= 1000:
		return 1
	case maxVal >= 100:
		return 2
	default:
		return PrecisionRaw
	}
}

func formatPrice(value float64, precision int) string {
	if precision == PrecisionRaw {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	s := strconv.FormatFloat(value, 'f', precision, 64)
	if precision > 0 {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func formatPlainFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
