package application

import (
	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
)

// CrontabEditApplication 編排改動 crontab 的用例。
//
// 寫入開關在這一層檢查，且在呼叫 service 之前 —— 唯讀模式下不該有任何寫入路徑
// 被走到，連讀取檔案都不必發生。
type CrontabEditApplication struct {
	crontabEditService *service.CrontabEditService
	clock              interfaces.IClock
	writeEnabled       bool
}

// NewCrontabEditApplication 建立 application。
func NewCrontabEditApplication(
	crontabEditService *service.CrontabEditService,
	clock interfaces.IClock,
	writeEnabled bool,
) *CrontabEditApplication {
	return &CrontabEditApplication{
		crontabEditService: crontabEditService,
		clock:              clock,
		writeEnabled:       writeEnabled,
	}
}

// CreateCronJob 新增一筆納管的 job。
func (application *CrontabEditApplication) CreateCronJob(
	scheduleExpression string,
	command string,
	description string,
	enabled bool,
) (dto.CronJobDto, error) {
	if !application.writeEnabled {
		return dto.CronJobDto{}, ErrCrontabWriteDisabled
	}

	return application.crontabEditService.CreateCronJob(
		scheduleExpression, command, description, enabled, application.clock.Now())
}

// UpdateCronJob 改寫一筆既有的 job。
func (application *CrontabEditApplication) UpdateCronJob(
	jobID string,
	scheduleExpression string,
	command string,
	description string,
	enabled bool,
) (dto.CronJobDto, error) {
	if !application.writeEnabled {
		return dto.CronJobDto{}, ErrCrontabWriteDisabled
	}

	return application.crontabEditService.UpdateCronJob(
		jobID, scheduleExpression, command, description, enabled, application.clock.Now())
}

// DeleteCronJob 移除一筆 job。
func (application *CrontabEditApplication) DeleteCronJob(jobID string) error {
	if !application.writeEnabled {
		return ErrCrontabWriteDisabled
	}

	return application.crontabEditService.DeleteCronJob(jobID)
}

// SetCronJobEnabled 啟用或停用一筆 job。
func (application *CrontabEditApplication) SetCronJobEnabled(jobID string, enabled bool) (dto.CronJobDto, error) {
	if !application.writeEnabled {
		return dto.CronJobDto{}, ErrCrontabWriteDisabled
	}

	return application.crontabEditService.SetCronJobEnabled(jobID, enabled, application.clock.Now())
}

// AdoptCronJob 把一筆 foreign job 轉為納管。
func (application *CrontabEditApplication) AdoptCronJob(jobID string) (dto.CronJobDto, error) {
	if !application.writeEnabled {
		return dto.CronJobDto{}, ErrCrontabWriteDisabled
	}

	return application.crontabEditService.AdoptCronJob(jobID, application.clock.Now())
}

// GetCrontabContent 回傳 crontab 原文。這是讀取用例，不受寫入開關影響。
func (application *CrontabEditApplication) GetCrontabContent() (string, error) {
	return application.crontabEditService.GetCrontabContent()
}

// WriteEnabled 回報這個部署是否允許改動 crontab，供 UI 決定要不要顯示編輯按鈕。
func (application *CrontabEditApplication) WriteEnabled() bool {
	return application.writeEnabled
}
