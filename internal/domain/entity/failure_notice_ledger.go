package entity

// FailureNoticeLedger 記住「哪些執行已經結算過」，據此挑出**新出現**的失敗。
//
// 它存在的理由只有一個：每 30 秒重看一次同一份資料，若不記得上次看到什麼，同一
// 次失敗就會每 30 秒通知一次，而那跟沒有通知一樣糟。
//
// 記憶是刻意只活在一次執行期間的。應用重啟即重新開始，這正是「離線期間的失敗
// 不補通知」要的語意 —— 那些是使用者不在的時候發生的事，圖示會說，通知不必。
type FailureNoticeLedger struct {
	settledRunIDs map[string]bool
	primed        bool
}

// NewFailureNoticeLedger 建立一份空的帳。
func NewFailureNoticeLedger() *FailureNoticeLedger {
	return &FailureNoticeLedger{settledRunIDs: map[string]bool{}}
}

// Reconcile 對一份現況結算，回傳這一輪**新出現**且該通知的失敗。
//
// 三條規則決定了它的正確性：
//
//   - 第一次結算只記錄、不通知。當時看到的失敗都是應用還沒開著時發生的。
//   - 讀不到來源的那一輪什麼都不做。此時我們對現況一無所知，洗掉記憶會讓恢復
//     之後真正新出現的失敗被誤判成舊的；發通知則是純粹瞎猜。
//   - 只有「已結束」的執行才算結算過。一筆還在跑的紀錄若被記下，它之後結束成
//     失敗時就再也不會被通知到。
//
// 記憶每輪整份取代而非累加，因此集合大小恆等於目前有紀錄的 job 數，不會隨著
// 應用開著的時間無限成長。
func (ledger *FailureNoticeLedger) Reconcile(status *DesktopStatus) []*FailureNotice {
	if status.Indicator() == StatusIndicatorUnavailable {
		return nil
	}

	settledNow := status.SettledRunIDs()

	if !ledger.primed {
		ledger.settledRunIDs = settledNow
		ledger.primed = true

		return nil
	}

	notices := make([]*FailureNotice, 0)
	for _, candidate := range status.FailureCandidates() {
		if !ledger.settledRunIDs[candidate.RunID()] {
			notices = append(notices, candidate)
		}
	}

	ledger.settledRunIDs = settledNow

	return notices
}
