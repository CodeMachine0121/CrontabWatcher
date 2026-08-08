package dto

import "time"

// DesktopStatusDto 是桌面形態一次刷新後的畫面資料。
type DesktopStatusDto struct {
	// Indicator 是選單列圖示的三態：normal／attention／unavailable。
	Indicator string `json:"indicator"`

	// UnavailableReason 說明為何什麼都答不出來。狀態正常時為空字串。
	UnavailableReason string `json:"unavailableReason"`

	Lines []JobStatusLineDto `json:"lines"`

	// OmittedLineCount 是因為超過上限而沒有列出的筆數。安靜地截斷會被讀成
	// 「總共就這些」，所以這個數字必須一起帶出去。
	OmittedLineCount int `json:"omittedLineCount"`
}

// JobStatusLineDto 是摘要中的一行。
type JobStatusLineDto struct {
	JobID               string `json:"jobId"`
	DisplayName         string `json:"displayName"`
	ScheduleDescription string `json:"scheduleDescription"`

	// NextRunAt 在已停用或只在開機時執行時為 nil。
	NextRunAt *time.Time `json:"nextRunAt"`

	Enabled bool `json:"enabled"`

	// Outcome 是最近一次執行的結果：succeeded／failed／running／unknown。
	Outcome string `json:"outcome"`

	NeedsAttention bool `json:"needsAttention"`
}

// FailureNoticeDto 是一則該送出的失敗通知。標題與內文已由領域層組好 —— 外殼
// 只負責送，不決定要說什麼。
type FailureNoticeDto struct {
	RunID string `json:"runId"`
	JobID string `json:"jobId"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// DesktopRefreshDto 是一次刷新的完整結果：該畫什麼、以及該通知什麼。
//
// 兩者一起回傳而不是分兩次呼叫：先後順序、以及「讀不到的時候不要通知」這條規則
// 都不該由外殼記得。
type DesktopRefreshDto struct {
	Status            DesktopStatusDto   `json:"status"`
	NewFailureNotices []FailureNoticeDto `json:"newFailureNotices"`
}
