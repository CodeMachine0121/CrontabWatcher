package interfaces

import "time"

// IClock 提供當下時間。
//
// 抽成介面是為了測試：排程與執行紀錄的斷言需要固定的時間基準。entity 的計算
// method 一律收 time.Time 參數，只有 service 會用到這個介面。
type IClock interface {
	Now() time.Time
}
