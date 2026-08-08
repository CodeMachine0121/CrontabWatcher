package application

import (
	"context"
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
)

// ManualTriggerApplication 編排「從瀏覽器立刻跑一次」的用例。
type ManualTriggerApplication struct {
	jobExecutionService *service.JobExecutionService
	enabled             bool
	timeout             time.Duration
}

// NewManualTriggerApplication 建立 application。
func NewManualTriggerApplication(
	jobExecutionService *service.JobExecutionService,
	enabled bool,
	timeout time.Duration,
) *ManualTriggerApplication {
	return &ManualTriggerApplication{
		jobExecutionService: jobExecutionService,
		enabled:             enabled,
		timeout:             timeout,
	}
}

// TriggerJobRun 立刻執行一次 job。
//
// 傳進來的 ctx 會被剝掉取消訊號：它是 HTTP 請求的 context，使用者關掉分頁就會被
// 取消。一個備份腳本不該因為瀏覽器關了而被砍到一半。逾時仍然有效 —— 那是我們自己
// 設的界線，而不是使用者的網路狀況。
func (application *ManualTriggerApplication) TriggerJobRun(ctx context.Context, jobID string) (dto.JobRunDto, error) {
	if !application.enabled {
		return dto.JobRunDto{}, ErrManualTriggerDisabled
	}

	return application.jobExecutionService.TriggerJobRun(
		context.WithoutCancel(ctx), jobID, application.timeout)
}

// RecordWrapperRun 執行並記錄一次由 cron 觸發的 job（wrapper subcommand 用）。
//
// 刻意不看 enabled 開關：那個開關管的是「瀏覽器上的按鈕」，不是 cron 的排程。
// 讓它擋住 wrapper 會直接讓使用者的 job 不執行。
func (application *ManualTriggerApplication) RecordWrapperRun(
	ctx context.Context,
	jobID string,
	command string,
) (dto.JobRunDto, error) {
	return application.jobExecutionService.RecordWrapperRun(ctx, jobID, command)
}
