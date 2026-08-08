package vo

import "time"

// JobStatusLine 是選單列摘要中的一行。
//
// 它是 DesktopStatus 歸納後的結果，純資料、無行為 —— 所有判斷（哪個結果算什麼、
// 怎麼排序、排到第幾筆為止）都已經在 entity 裡做完了。
type JobStatusLine struct {
	JobID       string
	DisplayName string

	// ScheduleDescription 是排程的人話說明，例如「每天 03:00」。
	ScheduleDescription string

	// NextRunAt 在已停用或無可預測下次執行時為 nil。刻意用指標而非零值：
	// 零時間會被畫成一個看起來很真的日期。
	NextRunAt *time.Time

	Enabled bool

	// Outcome 是最近一次執行的結果。未納管的 job 恆為 unknown。
	Outcome string

	// NeedsAttention 標出這一行是不是圖示轉為 attention 的原因。
	NeedsAttention bool
}
