package application

import (
	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
)

// CronJobApplication 編排排程條目的讀取用例。
type CronJobApplication struct {
	cronJobService *service.CronJobService
	clock          interfaces.IClock
}

// NewCronJobApplication 建立 application。
func NewCronJobApplication(cronJobService *service.CronJobService, clock interfaces.IClock) *CronJobApplication {
	return &CronJobApplication{
		cronJobService: cronJobService,
		clock:          clock,
	}
}

// ListCronJobs 回傳全部排程條目。
//
// 「現在幾點」在這一層取得並往下傳：時區是部署組態，domain 不該去問環境。
func (application *CronJobApplication) ListCronJobs() ([]dto.CronJobDto, error) {
	return application.cronJobService.ListCronJobs(application.clock.Now())
}

// GetCronJob 取單一排程條目。
func (application *CronJobApplication) GetCronJob(jobID string) (dto.CronJobDto, error) {
	return application.cronJobService.GetCronJob(jobID, application.clock.Now())
}
