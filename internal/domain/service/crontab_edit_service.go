package service

import (
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
)

// CrontabEditService 提供改動 crontab 的用例。
//
// 每個用例都是「讀取 → 在文件上做結構化修改 → 以樂觀鎖寫回」。輸入驗證一律發生在
// 讀取檔案之前，這樣不合法的請求連 I/O 都不會觸發。
type CrontabEditService struct {
	crontabDocumentRepository interfaces.ICrontabDocumentRepository
	identifierGenerator       interfaces.IIdentifierGenerator
	wrapperBinaryPath         string
	managedLogDirectory       string
}

// NewCrontabEditService 建立 service。
func NewCrontabEditService(
	crontabDocumentRepository interfaces.ICrontabDocumentRepository,
	identifierGenerator interfaces.IIdentifierGenerator,
	wrapperBinaryPath string,
	managedLogDirectory string,
) *CrontabEditService {
	return &CrontabEditService{
		crontabDocumentRepository: crontabDocumentRepository,
		identifierGenerator:       identifierGenerator,
		wrapperBinaryPath:         wrapperBinaryPath,
		managedLogDirectory:       managedLogDirectory,
	}
}

// CreateCronJob 新增一筆納管的 job。
func (service *CrontabEditService) CreateCronJob(
	scheduleExpression string,
	command string,
	description string,
	enabled bool,
	now time.Time,
) (dto.CronJobDto, error) {
	specification, err := entity.NewManagedJobSpecification(
		scheduleExpression, command, description, enabled, service.wrapperBinaryPath)
	if err != nil {
		return dto.CronJobDto{}, err
	}

	document, fingerprint, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return dto.CronJobDto{}, err
	}

	job, err := document.AppendManagedJob(service.identifierGenerator.NewIdentifier(), specification)
	if err != nil {
		return dto.CronJobDto{}, err
	}

	if err := service.crontabDocumentRepository.Save(document, fingerprint); err != nil {
		return dto.CronJobDto{}, err
	}

	return service.buildDto(job, now), nil
}

// UpdateCronJob 改寫一筆既有的 job。
//
// managed job 保持納管、foreign job 保持原狀 —— 使用者只是想改排程或指令，不該
// 因為按了儲存就被悄悄納管、輸出換到別的地方。
func (service *CrontabEditService) UpdateCronJob(
	jobID string,
	scheduleExpression string,
	command string,
	description string,
	enabled bool,
	now time.Time,
) (dto.CronJobDto, error) {
	specification, err := entity.NewManagedJobSpecification(
		scheduleExpression, command, description, enabled, service.wrapperBinaryPath)
	if err != nil {
		return dto.CronJobDto{}, err
	}

	document, fingerprint, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return dto.CronJobDto{}, err
	}

	job, err := document.ReplaceJob(jobID, specification)
	if err != nil {
		return dto.CronJobDto{}, err
	}

	if err := service.crontabDocumentRepository.Save(document, fingerprint); err != nil {
		return dto.CronJobDto{}, err
	}

	return service.buildDto(job, now), nil
}

// DeleteCronJob 移除一筆 job 及其 marker 註解。
func (service *CrontabEditService) DeleteCronJob(jobID string) error {
	document, fingerprint, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return err
	}

	if err := document.RemoveJob(jobID); err != nil {
		return err
	}

	return service.crontabDocumentRepository.Save(document, fingerprint)
}

// SetCronJobEnabled 啟用或停用一筆 job。
//
// 停用是註解掉、不是刪除，因此可以完全還原。
func (service *CrontabEditService) SetCronJobEnabled(
	jobID string,
	enabled bool,
	now time.Time,
) (dto.CronJobDto, error) {
	document, fingerprint, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return dto.CronJobDto{}, err
	}

	if err := document.SetJobEnabled(jobID, enabled); err != nil {
		return dto.CronJobDto{}, err
	}

	if err := service.crontabDocumentRepository.Save(document, fingerprint); err != nil {
		return dto.CronJobDto{}, err
	}

	// 識別碼在啟用／停用之間是穩定的：foreign job 的摘要算的是「剝掉開頭 # 之後」
	// 的排程與指令，而停用只動那個 #。
	job, err := document.MustFindJob(jobID)
	if err != nil {
		return dto.CronJobDto{}, err
	}

	return service.buildDto(job, now), nil
}

// AdoptCronJob 把一筆 foreign job 轉為納管。
func (service *CrontabEditService) AdoptCronJob(jobID string, now time.Time) (dto.CronJobDto, error) {
	document, fingerprint, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return dto.CronJobDto{}, err
	}

	job, err := document.AdoptJob(jobID, service.identifierGenerator.NewIdentifier(), service.wrapperBinaryPath)
	if err != nil {
		return dto.CronJobDto{}, err
	}

	if err := service.crontabDocumentRepository.Save(document, fingerprint); err != nil {
		return dto.CronJobDto{}, err
	}

	return service.buildDto(job, now), nil
}

// GetCrontabContent 回傳 crontab 檔案原文，供使用者對照。
func (service *CrontabEditService) GetCrontabContent() (string, error) {
	document, _, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return "", err
	}

	return document.Render(), nil
}

// buildDto 組出回應形狀。
//
// 刻意不附上最近一次執行紀錄：這是一個「剛剛改了什麼」的回應，不是歷史查詢，
// 多讀一次紀錄檔只是白花的 I/O。
func (service *CrontabEditService) buildDto(job *entity.CronJob, now time.Time) dto.CronJobDto {
	return buildCronJobDto(job, nil, service.managedLogDirectory, now)
}
