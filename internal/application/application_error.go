package application

import "errors"

// 組態驅動的用例前置條件。這些不是領域規則（crontab 本身沒有「唯讀」的概念），
// 而是部署方式決定的開關，因此放在 application 層而非 domain。
var (
	// ErrCrontabWriteDisabled 表示這個部署被設定為不改動 crontab（唯讀模式）。→ 403
	ErrCrontabWriteDisabled = errors.New("crontab writing is disabled in this deployment")

	// ErrManualTriggerDisabled 表示這個部署不允許從瀏覽器手動觸發。→ 403
	//
	// 唯讀模式預設關閉它：容器內的環境與 host 不同，跑出來的結果不可信。
	ErrManualTriggerDisabled = errors.New("manual triggering is disabled in this deployment")
)
