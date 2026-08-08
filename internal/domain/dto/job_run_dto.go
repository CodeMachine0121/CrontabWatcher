package dto

import "time"

// JobRunDto 是一次執行紀錄的回傳形狀。
type JobRunDto struct {
	RunID         string `json:"runId"`
	JobID         string `json:"jobId"`
	TriggerSource string `json:"triggerSource"`
	RunStatus     string `json:"runStatus"`

	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`

	// DurationMilliseconds 與 ExitCode 在未完成時為 nil。
	//
	// 用指標而非零值：exit code 的 0 代表成功，把「還不知道」寫成 0 就是謊報。
	DurationMilliseconds *int64 `json:"durationMilliseconds"`
	ExitCode             *int   `json:"exitCode"`

	OutputExcerpt   string `json:"outputExcerpt"`
	OutputTruncated bool   `json:"outputTruncated"`
}

// JobRunListDto 是執行歷史的回傳形狀。
type JobRunListDto struct {
	JobID string      `json:"jobId"`
	Runs  []JobRunDto `json:"runs"`

	// UnavailableReason 說明為何沒有紀錄可看（例如該 job 未納管）。有紀錄時為
	// 空字串。空清單加上原因，遠好過讓使用者對著空表格猜。
	UnavailableReason string `json:"unavailableReason"`
}

// JobLogDto 是 log 內容的回傳形狀。
type JobLogDto struct {
	JobID     string `json:"jobId"`
	LogSource string `json:"logSource"`
	FilePath  string `json:"filePath"`

	// Exists 與內容分開回報：「檔案不存在」是還沒跑過，「存在但空」是跑過而沒有
	// 輸出，對使用者是兩件不同的事。
	Exists    bool `json:"exists"`
	Truncated bool `json:"truncated"`

	LineCount int    `json:"lineCount"`
	Content   string `json:"content"`
}
