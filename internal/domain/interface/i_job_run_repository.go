package interfaces

import (
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

// IJobRunRepository 持久化執行紀錄（append-only JSON Lines）。
type IJobRunRepository interface {
	// Append 加入一筆新紀錄。
	Append(run *entity.JobRun) error

	// Update 以 RunID 覆寫既有紀錄，用於把 running 收尾成最終狀態。
	// 找不到該 RunID 時回傳 entity.ErrJobRunNotFound。
	Update(run *entity.JobRun) error

	// ListByJobID 回傳該 job 的紀錄，新到舊。limit 為 0 或負值表示不限。
	ListByJobID(jobID string, limit int) ([]*entity.JobRun, error)

	// LatestByJobIDs 一次取多個 job 各自最新的一筆紀錄。沒有紀錄的 job 不會
	// 出現在回傳的 map 裡。
	LatestByJobIDs(jobIDs []string) (map[string]*entity.JobRun, error)

	// HasRunningRun 回報該 job 是否已有執行中的紀錄。
	HasRunningRun(jobID string) (bool, error)

	// MarkRunningRunsAsInterrupted 把殘留的 running 紀錄標成無法判定，回傳處理
	// 筆數。server 啟動時呼叫一次。
	MarkRunningRunsAsInterrupted(interruptedAt time.Time) (int, error)
}
