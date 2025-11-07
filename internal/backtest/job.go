package backtest

import "time"

const (
	JobStatusPending = "pending"
	JobStatusRunning = "running"
	JobStatusDone    = "done"
	JobStatusFailed  = "failed"
	JobStatusPartial = "partial"
)

// FetchParams 描述一次拉取任务的请求参数。
type FetchParams struct {
	Exchange  string `json:"exchange"`
	Symbol    string `json:"symbol"`
	Timeframe string `json:"timeframe"`
	Start     int64  `json:"start"`
	End       int64  `json:"end"`
}

// FetchJob 用于在内存中跟踪任务进度。
type FetchJob struct {
	ID        string      `json:"id"`
	Status    string      `json:"status"`
	Params    FetchParams `json:"params"`
	Total     int64       `json:"total"`
	Completed int64       `json:"completed"`
	StartedAt time.Time   `json:"started_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	Message   string      `json:"message"`
	Warnings  []string    `json:"warnings"`
	Missing   []Gap       `json:"missing"`
}

func (j *FetchJob) copy() FetchJob {
	if j == nil {
		return FetchJob{}
	}
	out := *j
	out.Warnings = append([]string{}, j.Warnings...)
	out.Missing = append([]Gap{}, j.Missing...)
	return out
}
