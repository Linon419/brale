package backtest

import (
	"context"
)

// Gap 表示缺失的连续 K 线区间。
type Gap struct {
	From  int64 `json:"from"`
	To    int64 `json:"to"`
	Count int64 `json:"count"`
}

// IntegrityReport 描述指定区间的覆盖情况。
type IntegrityReport struct {
	Start       int64 `json:"start"`
	End         int64 `json:"end"`
	Expected    int64 `json:"expected"`
	Present     int64 `json:"present"`
	Gaps        []Gap `json:"gaps"`
	AlignedFrom int64 `json:"aligned_from"`
	AlignedTo   int64 `json:"aligned_to"`
}

func (r IntegrityReport) Complete() bool { return len(r.Gaps) == 0 }

func (s *Store) CheckIntegrity(ctx context.Context, symbol, timeframe string, tf Timeframe, start, end int64) (IntegrityReport, error) {
	alStart, alEnd := tf.AlignRange(start, end)
	report := IntegrityReport{
		Start:       start,
		End:         end,
		AlignedFrom: alStart,
		AlignedTo:   alEnd,
		Expected:    tf.ExpectedCandles(alStart, alEnd),
	}
	if report.Expected <= 0 {
		return report, nil
	}
	existing, err := s.LoadOpenTimes(ctx, symbol, timeframe, alStart, alEnd)
	if err != nil {
		return report, err
	}
	report.Present = int64(len(existing))

	step := tf.durationMillis()
	var gaps []Gap
	cursor := alStart
	idx := 0
	for cursor <= alEnd {
		match := idx < len(existing) && existing[idx] == cursor
		if match {
			idx++
			cursor += step
			continue
		}
		gapStart := cursor
		var missing int64
		for cursor <= alEnd {
			if idx < len(existing) && existing[idx] == cursor {
				break
			}
			cursor += step
			missing++
		}
		gapEnd := cursor - step
		if gapEnd < gapStart {
			gapEnd = gapStart
		}
		if missing > 0 {
			gaps = append(gaps, Gap{From: gapStart, To: gapEnd, Count: missing})
		}
		if cursor == gapStart {
			cursor += step
		}
	}
	report.Gaps = gaps
	return report, nil
}
