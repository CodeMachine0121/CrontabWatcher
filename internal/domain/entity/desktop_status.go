package entity

import (
	"sort"
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

// StatusIndicator 是選單列圖示回答的唯一問題：現在有沒有事要理。
type StatusIndicator string

const (
	// StatusIndicatorNormal 表示沒有已知的問題。注意「沒有已知的問題」不等於
	// 「一切都好」—— 未納管的 job 本來就無從得知。
	StatusIndicatorNormal StatusIndicator = "normal"
	// StatusIndicatorAttention 表示有納管的 job 最近一次執行失敗或逾時中止。
	StatusIndicatorAttention StatusIndicator = "attention"
	// StatusIndicatorUnavailable 表示連 crontab 都讀不到，因此什麼都答不出來。
	StatusIndicatorUnavailable StatusIndicator = "unavailable"
)

// NewStatusIndicator 正規化外來字串，未知值退回 normal。
//
// 退回 normal 而不是 attention：無法辨識的值代表程式出錯，拿它去嚇使用者、讓他
// 去找一個不存在的失敗 job，比安靜地正常顯示更糟。
func NewStatusIndicator(value string) StatusIndicator {
	switch StatusIndicator(value) {
	case StatusIndicatorAttention, StatusIndicatorUnavailable:
		return StatusIndicator(value)
	default:
		return StatusIndicatorNormal
	}
}

// LatestRunOutcome 是摘要上「最近一次結果」欄位的四種值。
type LatestRunOutcome string

const (
	// LatestRunOutcomeSucceeded：納管且最近一次成功。
	LatestRunOutcomeSucceeded LatestRunOutcome = "succeeded"
	// LatestRunOutcomeFailed：納管且最近一次失敗或逾時中止。
	LatestRunOutcomeFailed LatestRunOutcome = "failed"
	// LatestRunOutcomeRunning：納管且正在執行中。
	LatestRunOutcomeRunning LatestRunOutcome = "running"
	// LatestRunOutcomeUnknown：無從得知。未納管的 job 恆為此值，納管但從未執行
	// 過的 job 也是。**這不是失敗**，兩者的差別是本專案的核心紀律。
	LatestRunOutcomeUnknown LatestRunOutcome = "unknown"
)

// DesktopStatus 把「全部 job 加上各自最近一次執行」歸納成桌面形態需要的一切：
// 一個整體指示、一份摘要、以及待判定的失敗候選。
//
// 它同時承載「讀不到 crontab」這個狀態 —— 那在這個用例裡不是例外而是一種要顯示
// 出來的事實，因此它是這個 entity 的一種形態，不是一個錯誤。
type DesktopStatus struct {
	jobs              []*CronJob
	latestRuns        map[string]*JobRun
	now               time.Time
	unavailableReason string
}

// NewDesktopStatus 由 job 清單與各自最近一次執行建立狀態。
//
// now 由呼叫方帶入（帶著正確的時區），entity 不自己取時間 —— 排程計算因此完全
// 可測。
func NewDesktopStatus(jobs []*CronJob, latestRuns map[string]*JobRun, now time.Time) *DesktopStatus {
	if latestRuns == nil {
		latestRuns = map[string]*JobRun{}
	}

	return &DesktopStatus{jobs: jobs, latestRuns: latestRuns, now: now}
}

// NewUnavailableDesktopStatus 建立「讀不到 crontab」的狀態。
//
// 它刻意不帶任何 job：顯示一份過期的清單，會讓使用者以為那就是現況。
func NewUnavailableDesktopStatus(reason string) *DesktopStatus {
	return &DesktopStatus{latestRuns: map[string]*JobRun{}, unavailableReason: reason}
}

// Indicator 回答「現在有沒有事要理」。
//
// 只看每個納管 job 的**最近一次**執行，不累積歷史：一個修好了的 job 不該永遠掛
// 著紅點。未納管的 job 無論最近一次紀錄是什麼都不算數 —— 它按排程跑的那些執行
// 根本不經過我們，紀錄不能代表它。
func (status *DesktopStatus) Indicator() StatusIndicator {
	if status.unavailableReason != "" {
		return StatusIndicatorUnavailable
	}

	for _, job := range status.jobs {
		if status.outcomeOf(job) == LatestRunOutcomeFailed {
			return StatusIndicatorAttention
		}
	}

	return StatusIndicatorNormal
}

// UnavailableReason 說明為何什麼都答不出來。狀態正常時為空字串。
func (status *DesktopStatus) UnavailableReason() string {
	return status.unavailableReason
}

// Lines 回傳摘要行與**被略過的筆數**。limit 為 0 或負值表示不截斷。
//
// 排序是為了讓最該被看到的東西不會剛好被截掉：出事的排最前，接著是快要跑的，
// 不會再跑的（已停用、只在開機時跑）排最後。同一組內維持 crontab 上的原始順序，
// 使用者對自己的檔案有記憶。
//
// 回傳被略過的筆數而不是只回前 N 筆：安靜地截斷會被讀成「總共就這些」。
func (status *DesktopStatus) Lines(limit int) ([]vo.JobStatusLine, int) {
	if status.unavailableReason != "" {
		return []vo.JobStatusLine{}, 0
	}

	lines := make([]vo.JobStatusLine, 0, len(status.jobs))
	for _, job := range status.jobs {
		lines = append(lines, status.buildLine(job))
	}

	sort.SliceStable(lines, func(leftIndex int, rightIndex int) bool {
		return status.lessUrgent(lines[leftIndex], lines[rightIndex])
	})

	if limit > 0 && len(lines) > limit {
		return lines[:limit], len(lines) - limit
	}

	return lines, 0
}

// buildLine 把一個 job 攤平成摘要的一行。
func (status *DesktopStatus) buildLine(job *CronJob) vo.JobStatusLine {
	outcome := status.outcomeOf(job)

	line := vo.JobStatusLine{
		JobID:               job.JobID(),
		DisplayName:         job.DisplayName(),
		ScheduleDescription: job.Schedule().Describe(),
		Enabled:             job.Enabled(),
		Outcome:             string(outcome),
		NeedsAttention:      outcome == LatestRunOutcomeFailed,
	}

	// 已停用或只在開機時跑的 job 沒有下次執行時間。留 nil 而不是零時間 ——
	// 零時間會被畫成一個看起來很真的日期。
	if nextRunAt, predictable := job.NextRunAt(status.now); predictable {
		line.NextRunAt = &nextRunAt
	}

	return line
}

// lessUrgent 定義摘要的排序：需要注意的最前，然後是下次執行最近的，沒有下次執行
// 的最後。
func (status *DesktopStatus) lessUrgent(leftLine vo.JobStatusLine, rightLine vo.JobStatusLine) bool {
	if leftLine.NeedsAttention != rightLine.NeedsAttention {
		return leftLine.NeedsAttention
	}

	if (leftLine.NextRunAt == nil) != (rightLine.NextRunAt == nil) {
		return leftLine.NextRunAt != nil
	}

	if leftLine.NextRunAt == nil {
		return false
	}

	return leftLine.NextRunAt.Before(*rightLine.NextRunAt)
}

// outcomeOf 判定單一 job 的最近一次結果。
func (status *DesktopStatus) outcomeOf(job *CronJob) LatestRunOutcome {
	if !job.IsManaged() {
		return LatestRunOutcomeUnknown
	}

	latestRun, recorded := status.latestRuns[job.JobID()]
	if !recorded || latestRun == nil {
		return LatestRunOutcomeUnknown
	}

	switch latestRun.RunStatus() {
	case RunStatusSucceeded:
		return LatestRunOutcomeSucceeded
	case RunStatusFailed, RunStatusTimedOut:
		return LatestRunOutcomeFailed
	case RunStatusRunning:
		return LatestRunOutcomeRunning
	default:
		return LatestRunOutcomeUnknown
	}
}
