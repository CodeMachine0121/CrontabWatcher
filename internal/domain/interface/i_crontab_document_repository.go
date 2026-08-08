package interfaces

import "github.com/james-hsueh/crontab-watcher/internal/domain/entity"

// ICrontabDocumentRepository 讀寫 crontab 檔案。
//
// 不吃 context：這全是本機檔案 I/O，沒有可取消的網路等待，加上 ctx 只是雜訊。
type ICrontabDocumentRepository interface {
	// Load 讀取 crontab 檔並回傳文件，以及讀取當下的版本指紋。
	// 檔案不存在時回傳空文件而非錯誤 —— 那是首次啟動的正常狀態。
	Load() (*entity.CrontabDocument, string, error)

	// Save 以樂觀鎖寫回檔案。expectedFingerprint 與當下檔案不符時回傳
	// entity.ErrCrontabChangedExternally，絕不覆蓋使用者用 crontab -e 的手改。
	Save(document *entity.CrontabDocument, expectedFingerprint string) error
}
