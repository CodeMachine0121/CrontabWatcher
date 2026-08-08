package dto

import "time"

// CronJobDto 是 domain 對 application 回傳的排程條目形狀。
type CronJobDto struct {
	JobID string `json:"jobId"`
	// Origin 為 managed 或 foreign。它決定了 LatestRun 與執行歷史有多可信。
	Origin  string `json:"origin"`
	Enabled bool   `json:"enabled"`

	ScheduleExpression         string `json:"scheduleExpression"`
	ScheduleOriginalExpression string `json:"scheduleOriginalExpression"`
	ScheduleDescription        string `json:"scheduleDescription"`

	// Command 是實際會被執行的那道指令（managed job 已剝掉 wrapper）。
	Command string `json:"command"`
	// RawCommand 是 crontab 條目上的原文，供對照。
	RawCommand string `json:"rawCommand"`

	// NextRunAt 在已停用或 @reboot 時為 nil —— 這兩種情況給出時間都是誤導。
	NextRunAt          *time.Time `json:"nextRunAt"`
	NextRunPredictable bool       `json:"nextRunPredictable"`

	LogSource   string `json:"logSource"`
	LogFilePath string `json:"logFilePath"`

	// LatestRun 在沒有任何執行紀錄時為 nil。
	LatestRun *JobRunDto `json:"latestRun"`

	// LineNumber 是在 crontab 檔案中的行號（1-based）。
	LineNumber int `json:"lineNumber"`
}
