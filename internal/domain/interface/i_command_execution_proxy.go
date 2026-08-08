package interfaces

import (
	"context"
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

// ICommandExecutionProxy 以 shell 執行一道指令。
//
// 吃 context 是因為逾時取消就是它的核心語意，不是為了統一簽章。
type ICommandExecutionProxy interface {
	// Execute 執行指令並回傳合併後的輸出與 exit code。
	// timeout 為 0 表示不套逾時（由 cron 排程觸發的執行該跑多久就跑多久）。
	// 指令本身失敗（非 0 exit code、找不到指令）不算錯誤，錯誤只表示我們自己
	// 無法完成執行這件事。
	Execute(ctx context.Context, command string, timeout time.Duration) (vo.CommandExecutionResult, error)
}
