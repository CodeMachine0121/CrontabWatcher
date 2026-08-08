package application

import (
	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
)

const (
	// MaximumLogTailLines 是 ?lines= 的硬上限。開放到無限大等於讓一個 query
	// 參數就能把記憶體吃光。
	MaximumLogTailLines = 5000
	// MaximumJobRunHistoryLimit 是 ?limit= 的硬上限，理由同上。
	MaximumJobRunHistoryLimit = 1000
	// defaultJobRunHistoryLimit 是未指定 limit 時回傳的筆數。
	defaultJobRunHistoryLimit = 50
)

// JobRunApplication 編排執行歷史與 log 檢視的用例。
type JobRunApplication struct {
	jobRunService       *service.JobRunService
	defaultLogTailLines int
}

// NewJobRunApplication 建立 application。defaultLogTailLines 來自組態。
func NewJobRunApplication(jobRunService *service.JobRunService, defaultLogTailLines int) *JobRunApplication {
	return &JobRunApplication{
		jobRunService:       jobRunService,
		defaultLogTailLines: defaultLogTailLines,
	}
}

// ListJobRuns 回傳執行歷史。requestedLimit 為 0 表示採用預設值。
func (application *JobRunApplication) ListJobRuns(jobID string, requestedLimit int) (dto.JobRunListDto, error) {
	return application.jobRunService.ListJobRuns(jobID, clampLimit(requestedLimit, defaultJobRunHistoryLimit, MaximumJobRunHistoryLimit))
}

// TailJobLog 回傳 log 尾巴。requestedLines 為 0 表示採用組態的預設行數。
func (application *JobRunApplication) TailJobLog(jobID string, requestedLines int) (dto.JobLogDto, error) {
	return application.jobRunService.TailJobLog(jobID, clampLimit(requestedLines, application.defaultLogTailLines, MaximumLogTailLines))
}

// clampLimit 把外來的數量收進合理範圍。
//
// 超出上限時取上限而非報錯：使用者要一萬行、給他五千行是可用的結果；為此回 400
// 只是把一個能滿足的請求變成失敗。
func clampLimit(requested int, defaultValue int, maximum int) int {
	if requested <= 0 {
		requested = defaultValue
	}

	if requested > maximum {
		return maximum
	}

	if requested < 1 {
		return 1
	}

	return requested
}
