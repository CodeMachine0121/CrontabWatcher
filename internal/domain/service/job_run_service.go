package service

import (
	"fmt"

	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
)

// foreignJobHistoryUnavailableReason 說明為何一個未納管的 job 沒有執行歷史。
const foreignJobHistoryUnavailableReason = "this job is not managed by crontab-watcher, " +
	"so cron runs leave no exit code or per-run record; adopt it to start recording runs"

// JobRunService 提供執行歷史與 log 內容的讀取用例。
type JobRunService struct {
	crontabDocumentRepository interfaces.ICrontabDocumentRepository
	jobRunRepository          interfaces.IJobRunRepository
	jobLogRepository          interfaces.IJobLogRepository
	managedLogDirectory       string
}

// NewJobRunService 建立 service。
func NewJobRunService(
	crontabDocumentRepository interfaces.ICrontabDocumentRepository,
	jobRunRepository interfaces.IJobRunRepository,
	jobLogRepository interfaces.IJobLogRepository,
	managedLogDirectory string,
) *JobRunService {
	return &JobRunService{
		crontabDocumentRepository: crontabDocumentRepository,
		jobRunRepository:          jobRunRepository,
		jobLogRepository:          jobLogRepository,
		managedLogDirectory:       managedLogDirectory,
	}
}

// ListJobRuns 回傳該 job 的執行歷史，新到舊。
//
// 未納管的 job 回空清單並附上原因，而不是假裝有歷史 —— cron 觸發的執行不經過
// wrapper，根本沒有 exit code 可記。
func (service *JobRunService) ListJobRuns(jobID string, limit int) (dto.JobRunListDto, error) {
	job, err := service.findJob(jobID)
	if err != nil {
		return dto.JobRunListDto{}, err
	}

	if !job.IsManaged() {
		return dto.JobRunListDto{
			JobID:             jobID,
			Runs:              []dto.JobRunDto{},
			UnavailableReason: foreignJobHistoryUnavailableReason,
		}, nil
	}

	runs, err := service.jobRunRepository.ListByJobID(jobID, limit)
	if err != nil {
		return dto.JobRunListDto{}, err
	}

	return dto.JobRunListDto{
		JobID: jobID,
		Runs:  buildJobRunDtos(runs),
	}, nil
}

// TailJobLog 讀取該 job 的 log 尾巴。
//
// 無 log 可讀時回 ErrJobLogUnavailable，而不是回一個空字串 —— 空字串會被讀成
// 「跑過但沒有輸出」，那與「根本無從得知」是完全不同的事實。
func (service *JobRunService) TailJobLog(jobID string, lines int) (dto.JobLogDto, error) {
	job, err := service.findJob(jobID)
	if err != nil {
		return dto.JobLogDto{}, err
	}

	if job.LogSource() == entity.LogSourceNone {
		return dto.JobLogDto{}, fmt.Errorf("%w: %s has no output destination we can read", entity.ErrJobLogUnavailable, jobID)
	}

	logFilePath := job.ResolveLogFilePath(service.managedLogDirectory)

	logTail, err := service.jobLogRepository.Tail(logFilePath, lines)
	if err != nil {
		return dto.JobLogDto{}, err
	}

	return dto.JobLogDto{
		JobID:     jobID,
		LogSource: string(job.LogSource()),
		FilePath:  logFilePath,
		Exists:    logTail.Exists(),
		Truncated: logTail.Truncated(),
		LineCount: logTail.LineCount(),
		Content:   logTail.Content(),
	}, nil
}

func (service *JobRunService) findJob(jobID string) (*entity.CronJob, error) {
	document, _, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return nil, err
	}

	return document.MustFindJob(jobID)
}
