package entity

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// JobRunOutputExcerptMaxBytes 是執行紀錄裡保留的輸出上限。
//
// runs.jsonl 要能整份載入記憶體做歷史查詢，不能讓單筆巨大輸出把它撐爆；完整
// 內容留在 log 檔裡。
const JobRunOutputExcerptMaxBytes = 8 * 1024

// interruptedOutputExcerpt 是被重啟中斷的執行紀錄留下的說明。
const interruptedOutputExcerpt = "interrupted by restart"

// TriggerSource 說明一次執行是誰觸發的。
type TriggerSource string

const (
	// TriggerSourceSchedule 是由 cron 依排程觸發。
	TriggerSourceSchedule TriggerSource = "schedule"
	// TriggerSourceManual 是使用者從瀏覽器手動觸發。
	TriggerSourceManual TriggerSource = "manual"
)

// NewTriggerSource 正規化外來字串，未知值退回 schedule。
func NewTriggerSource(value string) TriggerSource {
	if TriggerSource(value) == TriggerSourceManual {
		return TriggerSourceManual
	}
	return TriggerSourceSchedule
}

// RunStatus 是一次執行的結果狀態。
type RunStatus string

const (
	// RunStatusRunning 表示還在執行中。
	RunStatusRunning RunStatus = "running"
	// RunStatusSucceeded 表示 exit code 為 0。
	RunStatusSucceeded RunStatus = "succeeded"
	// RunStatusFailed 表示 exit code 非 0。
	RunStatusFailed RunStatus = "failed"
	// RunStatusTimedOut 表示因逾時被強制收掉。
	RunStatusTimedOut RunStatus = "timedOut"
	// RunStatusUnknown 表示無法判定 —— foreign job 沒有 exit code，或紀錄被
	// 重啟中斷。這是非法值的正規化目標。
	RunStatusUnknown RunStatus = "unknown"
)

// NewRunStatus 正規化外來字串，未知值退回 unknown。
//
// 退回 unknown 而不是 failed 或 succeeded 是刻意的：把「不知道」講成「成功」或
// 「失敗」都是謊報。
func NewRunStatus(value string) RunStatus {
	switch RunStatus(value) {
	case RunStatusRunning, RunStatusSucceeded, RunStatusFailed, RunStatusTimedOut:
		return RunStatus(value)
	default:
		return RunStatusUnknown
	}
}

// JobRun 是一次執行的紀錄。
type JobRun struct {
	runID           string
	jobID           string
	triggerSource   TriggerSource
	runStatus       RunStatus
	startedAt       time.Time
	finishedAt      time.Time
	exitCode        int
	exitCodeKnown   bool
	outputExcerpt   string
	outputTruncated bool
}

// NewJobRun 建立一筆執行中的紀錄。
//
// 紀錄在指令開始執行「之前」就先落地，這樣程序被砍掉時仍留得下痕跡；代價是
// 檔案裡可能出現孤兒 running 紀錄，由啟動時的掃描處理。
func NewJobRun(runID string, jobID string, triggerSource TriggerSource, startedAt time.Time) *JobRun {
	return &JobRun{
		runID:         runID,
		jobID:         jobID,
		triggerSource: NewTriggerSource(string(triggerSource)),
		runStatus:     RunStatusRunning,
		startedAt:     startedAt,
	}
}

// RestoreJobRun 從持久化的欄位重建紀錄。repository 讀 runs.jsonl 時用它 ——
// 外來字串一律經過正規化，不信任檔案裡的內容。
func RestoreJobRun(
	runID string,
	jobID string,
	triggerSource string,
	runStatus string,
	startedAt time.Time,
	finishedAt time.Time,
	exitCode int,
	exitCodeKnown bool,
	outputExcerpt string,
	outputTruncated bool,
) *JobRun {
	return &JobRun{
		runID:           runID,
		jobID:           jobID,
		triggerSource:   NewTriggerSource(triggerSource),
		runStatus:       NewRunStatus(runStatus),
		startedAt:       startedAt,
		finishedAt:      finishedAt,
		exitCode:        exitCode,
		exitCodeKnown:   exitCodeKnown,
		outputExcerpt:   outputExcerpt,
		outputTruncated: outputTruncated,
	}
}

// Finish 收尾一筆執行紀錄。
//
// 逾時優先於 exit code：被 kill 的程序有可能剛好回報 0，但那個 0 沒有意義。
func (run *JobRun) Finish(finishedAt time.Time, exitCode int, timedOut bool, output string) {
	run.finishedAt = finishedAt
	run.exitCode = exitCode
	run.exitCodeKnown = true
	run.outputExcerpt, run.outputTruncated = buildOutputExcerpt(output)

	switch {
	case timedOut:
		run.runStatus = RunStatusTimedOut
	case exitCode == 0:
		run.runStatus = RunStatusSucceeded
	default:
		run.runStatus = RunStatusFailed
	}
}

// MarkInterrupted 把一筆執行中的紀錄標成無法判定。server 啟動時用它清理殘留的
// running 紀錄 —— 留著它們假裝還在跑，會讓「這個 job 是不是卡住了」永遠問不出
// 答案。
func (run *JobRun) MarkInterrupted(finishedAt time.Time) {
	run.finishedAt = finishedAt
	run.runStatus = RunStatusUnknown
	run.exitCodeKnown = false
	run.outputExcerpt = interruptedOutputExcerpt
	run.outputTruncated = false
}

// RunID 回傳這次執行的識別碼。
func (run *JobRun) RunID() string {
	return run.runID
}

// JobID 回傳所屬 job 的識別碼。
func (run *JobRun) JobID() string {
	return run.jobID
}

// TriggerSource 回傳觸發來源。
func (run *JobRun) TriggerSource() TriggerSource {
	return run.triggerSource
}

// RunStatus 回傳結果狀態。
func (run *JobRun) RunStatus() RunStatus {
	return run.runStatus
}

// StartedAt 回傳開始時刻。
func (run *JobRun) StartedAt() time.Time {
	return run.startedAt
}

// FinishedAt 回傳結束時刻；未結束時第二個回傳值為 false。
func (run *JobRun) FinishedAt() (time.Time, bool) {
	if run.finishedAt.IsZero() {
		return time.Time{}, false
	}

	return run.finishedAt, true
}

// ExitCode 回傳 exit code；未知時第二個回傳值為 false。
//
// 刻意不用 0 當「沒有值」的表示 —— 0 是成功的意思，把未知講成成功是最糟的謊。
func (run *JobRun) ExitCode() (int, bool) {
	return run.exitCode, run.exitCodeKnown
}

// Duration 回傳耗時；未結束時第二個回傳值為 false。
func (run *JobRun) Duration() (time.Duration, bool) {
	if run.finishedAt.IsZero() {
		return 0, false
	}

	return run.finishedAt.Sub(run.startedAt), true
}

// IsFinished 回報這筆紀錄是否已收尾。
func (run *JobRun) IsFinished() bool {
	return run.runStatus != RunStatusRunning
}

// Succeeded 回報這次執行是否成功。
func (run *JobRun) Succeeded() bool {
	return run.runStatus == RunStatusSucceeded
}

// OutputExcerpt 回傳輸出摘要。
func (run *JobRun) OutputExcerpt() string {
	return run.outputExcerpt
}

// OutputTruncated 回報摘要是否被截斷。
func (run *JobRun) OutputTruncated() bool {
	return run.outputTruncated
}

// BuildLogHeader 產生寫進 log 檔的起始分隔行，讓人肉閱讀時能切分逐次執行。
func (run *JobRun) BuildLogHeader() string {
	return fmt.Sprintf("===== cronwatch run runId=%s trigger=%s started=%s =====\n",
		run.runID, run.triggerSource, run.startedAt.Format(time.RFC3339))
}

// BuildLogFooter 產生寫進 log 檔的結束分隔行。
func (run *JobRun) BuildLogFooter() string {
	exitCodeText := "unknown"
	if run.exitCodeKnown {
		exitCodeText = fmt.Sprintf("%d", run.exitCode)
	}

	durationText := "unknown"
	if duration, known := run.Duration(); known {
		durationText = duration.String()
	}

	return fmt.Sprintf("===== cronwatch run runId=%s exit=%s duration=%s status=%s =====\n",
		run.runID, exitCodeText, durationText, run.runStatus)
}

// buildOutputExcerpt 把輸出截成摘要。
//
// 保留尾端而不是開頭：錯誤訊息幾乎總在最後。截斷點會退到合法的 UTF-8 邊界，
// 否則半個字元在網頁上就是亂碼。
func buildOutputExcerpt(output string) (excerpt string, truncated bool) {
	if len(output) <= JobRunOutputExcerptMaxBytes {
		return output, false
	}

	tail := output[len(output)-JobRunOutputExcerptMaxBytes:]
	for len(tail) > 0 && !utf8.ValidString(tail) {
		tail = tail[1:]
	}

	return strings.TrimPrefix(tail, "\n"), true
}
