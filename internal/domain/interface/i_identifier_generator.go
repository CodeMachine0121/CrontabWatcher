package interfaces

// IIdentifierGenerator 產生 JobID 與 RunID。
//
// 抽成介面是為了測試：寫入 crontab 與執行紀錄的斷言需要可預測的識別碼，否則
// 只能寫得很鬆。
type IIdentifierGenerator interface {
	NewIdentifier() string
}
