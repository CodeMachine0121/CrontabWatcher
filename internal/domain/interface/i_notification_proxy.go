package interfaces

// INotificationProxy 送出一則系統通知。
//
// 它刻意只收已經組好的文字：什麼算失敗、該說哪幾個字，都是領域層的事。這條縫
// 也是「哪天要改送到別的地方（Slack、email）」唯一需要動的地方。
type INotificationProxy interface {
	// Notify 送出一則通知。送不出去回錯誤 —— 靜靜地失敗會讓使用者以為一切正常。
	Notify(title string, body string) error
}
