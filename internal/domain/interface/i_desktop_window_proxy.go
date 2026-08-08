package interfaces

// IDesktopWindowProxy 開啟桌面形態的完整視窗。
//
// 介面只有「開啟」而沒有「已經開了嗎」：呼叫方不該需要知道視窗現在是什麼狀態，
// 它只知道自己要讓某個網址被看見。已經開著時該重用還是新開，是實作的事。
type IDesktopWindowProxy interface {
	// Open 讓 targetURL 出現在完整視窗裡。視窗已開著時重用它，不另開第二個。
	Open(targetURL string) error

	// Close 收掉視窗。應用結束時呼叫。
	Close()
}
