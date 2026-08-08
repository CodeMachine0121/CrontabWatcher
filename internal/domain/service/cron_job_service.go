package service

import (
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
)

// CronJobService 提供排程條目的讀取用例。
//
// 每次呼叫都重讀 crontab 檔案、不做快取：那是一個使用者也會用 crontab -e 改的
// 檔案，快取只會製造「畫面上的」與「實際上的」不一致。
type CronJobService struct {
	crontabDocumentRepository interfaces.ICrontabDocumentRepository
	jobRunRepository          interfaces.IJobRunRepository
	managedLogDirectory       string
}

// NewCronJobService 建立 service。
func NewCronJobService(
	crontabDocumentRepository interfaces.ICrontabDocumentRepository,
	jobRunRepository interfaces.IJobRunRepository,
	managedLogDirectory string,
) *CronJobService {
	return &CronJobService{
		crontabDocumentRepository: crontabDocumentRepository,
		jobRunRepository:          jobRunRepository,
		managedLogDirectory:       managedLogDirectory,
	}
}

// ListCronJobs 回傳全部排程條目，含下次執行時間與最近一次執行紀錄。
//
// now 由 application 傳入（帶著正確的時區），service 不自己取時間 —— 這讓排程
// 計算完全可測。
func (service *CronJobService) ListCronJobs(now time.Time) ([]dto.CronJobDto, error) {
	document, _, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return nil, err
	}

	return service.buildJobDtos(document.Jobs(), now)
}

// GetCronJob 取單一排程條目。
func (service *CronJobService) GetCronJob(jobID string, now time.Time) (dto.CronJobDto, error) {
	document, _, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return dto.CronJobDto{}, err
	}

	job, err := document.MustFindJob(jobID)
	if err != nil {
		return dto.CronJobDto{}, err
	}

	jobDtos, err := service.buildJobDtos([]*entity.CronJob{job}, now)
	if err != nil {
		return dto.CronJobDto{}, err
	}

	return jobDtos[0], nil
}

func (service *CronJobService) buildJobDtos(jobs []*entity.CronJob, now time.Time) ([]dto.CronJobDto, error) {
	jobIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.JobID())
	}

	// 讀取失敗一律往上拋，不降級成「沒有紀錄」—— 那會讓一個實際上跑失敗的 job
	// 在頁面上看起來只是還沒跑過。
	latestRuns, err := service.jobRunRepository.LatestByJobIDs(jobIDs)
	if err != nil {
		return nil, err
	}

	jobDtos := make([]dto.CronJobDto, 0, len(jobs))
	for _, job := range jobs {
		jobDtos = append(jobDtos, buildCronJobDto(job, latestRuns[job.JobID()], service.managedLogDirectory, now))
	}

	return jobDtos, nil
}
