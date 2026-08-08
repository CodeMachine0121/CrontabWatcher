package interfaces

import "github.com/james-hsueh/crontab-watcher/internal/domain/vo"

// IJobLogRepository 讀寫 job 的 log 檔。
type IJobLogRepository interface {
	// Tail 從檔尾往回讀最多 lines 行。檔案不存在時回傳 exists 為 false 的結果
	// 而非錯誤 —— job 還沒跑過是正常狀態。
	Tail(filePath string, lines int) (vo.JobLogTail, error)

	// Append 把內容附加到 log 檔尾端，必要時建立父目錄。
	Append(filePath string, content string) error
}
