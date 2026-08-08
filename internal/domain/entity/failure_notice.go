package entity

import "fmt"

// FailureKind 區分兩種壞消息。它們的成因與該做的事不同，因此通知上必須看得出
// 差別：一個是指令自己回報了非 0，一個是它跑太久而被我們收掉。
type FailureKind string

const (
	// FailureKindFailed：指令執行完畢但回報非 0 的結束碼。
	FailureKindFailed FailureKind = "failed"
	// FailureKindTimedOut：執行超過時限，被強制中止。
	FailureKindTimedOut FailureKind = "timedOut"
)

// NewFailureKind 正規化外來字串，未知值退回 failed。
func NewFailureKind(value string) FailureKind {
	if FailureKind(value) == FailureKindTimedOut {
		return FailureKindTimedOut
	}

	return FailureKindFailed
}

// FailureNotice 是一則待發出的失敗通知。
//
// 通知的文字掛在這裡而不是在外殼上：使用者不打開任何東西就只會看到這幾個字，
// 它們得說清楚是哪個 job、出了哪一種事，那是領域知識而不是排版。
type FailureNotice struct {
	runID          string
	jobID          string
	jobDisplayName string
	kind           FailureKind
	exitCode       int
	exitCodeKnown  bool
}

// NewFailureNotice 建立一則通知。
func NewFailureNotice(
	runID string,
	jobID string,
	jobDisplayName string,
	kind FailureKind,
	exitCode int,
	exitCodeKnown bool,
) *FailureNotice {
	return &FailureNotice{
		runID:          runID,
		jobID:          jobID,
		jobDisplayName: jobDisplayName,
		kind:           NewFailureKind(string(kind)),
		exitCode:       exitCode,
		exitCodeKnown:  exitCodeKnown,
	}
}

// RunID 回傳出事的那一次執行。通知以「一次執行」為單位，不是以 job 為單位 ——
// 同一個 job 的兩次失敗是兩件事。
func (notice *FailureNotice) RunID() string {
	return notice.runID
}

// JobID 回傳出事的 job。
func (notice *FailureNotice) JobID() string {
	return notice.jobID
}

// Kind 回傳失敗的種類。
func (notice *FailureNotice) Kind() FailureKind {
	return notice.kind
}

// NotificationTitle 是通知的標題：一眼看出是哪個 job、出了哪一種事。
func (notice *FailureNotice) NotificationTitle() string {
	if notice.kind == FailureKindTimedOut {
		return fmt.Sprintf("排程逾時中止：%s", notice.jobDisplayName)
	}

	return fmt.Sprintf("排程失敗：%s", notice.jobDisplayName)
}

// NotificationBody 是通知的內文。
//
// 結束碼未知時說「未知」而不是印出 0 —— 0 是成功的意思，把不知道講成成功是最糟
// 的謊，這條紀律與執行紀錄那邊完全一致。
func (notice *FailureNotice) NotificationBody() string {
	if notice.kind == FailureKindTimedOut {
		return "執行超過時限，已被中止"
	}

	if !notice.exitCodeKnown {
		return "結束碼未知"
	}

	return fmt.Sprintf("結束碼 %d", notice.exitCode)
}
