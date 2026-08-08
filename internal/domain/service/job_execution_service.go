package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
)

// JobExecutionService 執行 job 並落地紀錄。
//
// 手動觸發與 wrapper 走同一條執行路徑，只在三件事上不同：觸發來源、是否套逾時、
// 以及「紀錄寫不進去」時要不要中止。
type JobExecutionService struct {
	crontabDocumentRepository interfaces.ICrontabDocumentRepository
	jobRunRepository          interfaces.IJobRunRepository
	jobLogRepository          interfaces.IJobLogRepository
	commandExecutionProxy     interfaces.ICommandExecutionProxy
	identifierGenerator       interfaces.IIdentifierGenerator
	clock                     interfaces.IClock
	managedLogDirectory       string
}

// NewJobExecutionService 建立 service。
func NewJobExecutionService(
	crontabDocumentRepository interfaces.ICrontabDocumentRepository,
	jobRunRepository interfaces.IJobRunRepository,
	jobLogRepository interfaces.IJobLogRepository,
	commandExecutionProxy interfaces.ICommandExecutionProxy,
	identifierGenerator interfaces.IIdentifierGenerator,
	clock interfaces.IClock,
	managedLogDirectory string,
) *JobExecutionService {
	return &JobExecutionService{
		crontabDocumentRepository: crontabDocumentRepository,
		jobRunRepository:          jobRunRepository,
		jobLogRepository:          jobLogRepository,
		commandExecutionProxy:     commandExecutionProxy,
		identifierGenerator:       identifierGenerator,
		clock:                     clock,
		managedLogDirectory:       managedLogDirectory,
	}
}

// TriggerJobRun 立刻執行一次 job（手動觸發）。
//
// 記不下來就先不要跑：使用者按了按鈕卻得不到任何結果回饋，比根本沒跑更糟。
func (service *JobExecutionService) TriggerJobRun(
	ctx context.Context,
	jobID string,
	timeout time.Duration,
) (dto.JobRunDto, error) {
	document, _, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return dto.JobRunDto{}, err
	}

	job, err := document.MustFindJob(jobID)
	if err != nil {
		return dto.JobRunDto{}, err
	}

	alreadyRunning, err := service.jobRunRepository.HasRunningRun(jobID)
	if err != nil {
		return dto.JobRunDto{}, err
	}
	if alreadyRunning {
		return dto.JobRunDto{}, fmt.Errorf("%w: %s", entity.ErrJobRunAlreadyRunning, jobID)
	}

	run := entity.NewJobRun(
		service.identifierGenerator.NewIdentifier(), jobID, entity.TriggerSourceManual, service.clock.Now())

	if err := service.jobRunRepository.Append(run); err != nil {
		return dto.JobRunDto{}, err
	}

	return service.executeAndFinish(ctx, run, job.InnerCommand(), service.resolveLogFilePath(job), timeout)
}

// RecordWrapperRun 執行並記錄一次由 cron 觸發的 job（wrapper subcommand）。
//
// 與手動觸發的三個關鍵差異：
//   - 不讀 crontab —— 指令由 argv 帶入，少一個可能讓 job 跑不起來的失敗點
//   - 不做並發檢查 —— 是 cron 決定要跑的，重疊執行是使用者自己的排程問題
//   - 不套逾時，且紀錄寫不進去也照跑 —— 我們的簿記壞掉不該把使用者的排程弄壞
func (service *JobExecutionService) RecordWrapperRun(
	ctx context.Context,
	jobID string,
	command string,
) (dto.JobRunDto, error) {
	run := entity.NewJobRun(
		service.identifierGenerator.NewIdentifier(), jobID, entity.TriggerSourceSchedule, service.clock.Now())

	// 刻意忽略錯誤：紀錄不下來也要跑。錯誤會在收尾時一併反映到輸出摘要裡。
	appendProblem := service.jobRunRepository.Append(run)

	logFilePath := filepath.Join(service.managedLogDirectory, jobID+".log")

	runDto, err := service.executeAndFinish(ctx, run, command, logFilePath, 0)
	if err != nil {
		return runDto, err
	}

	if appendProblem != nil {
		runDto.OutputExcerpt = appendBookkeepingProblem(runDto.OutputExcerpt, appendProblem)
	}

	return runDto, nil
}

// executeAndFinish 執行指令、寫 log、收尾紀錄。
func (service *JobExecutionService) executeAndFinish(
	ctx context.Context,
	run *entity.JobRun,
	command string,
	logFilePath string,
	timeout time.Duration,
) (dto.JobRunDto, error) {
	loggingProblems := make([]error, 0, 3)

	if err := service.jobLogRepository.Append(logFilePath, run.BuildLogHeader()); err != nil {
		loggingProblems = append(loggingProblems, err)
	}

	result, executionErr := service.commandExecutionProxy.Execute(ctx, command, timeout)

	if executionErr != nil {
		// 我們沒能把這次執行走完。不能標成 failed —— 那會讓使用者以為是他的 job
		// 有問題；標成 unknown 才誠實。
		run.MarkInterrupted(service.clock.Now())
		_ = service.jobRunRepository.Update(run)

		return buildJobRunDto(run), fmt.Errorf("executing job %s: %w", run.JobID(), executionErr)
	}

	if result.Output() != "" {
		if err := service.jobLogRepository.Append(logFilePath, result.Output()); err != nil {
			loggingProblems = append(loggingProblems, err)
		}
	}

	run.Finish(service.clock.Now(), result.ExitCode(), result.TimedOut(), result.Output())

	if err := service.jobLogRepository.Append(logFilePath, run.BuildLogFooter()); err != nil {
		loggingProblems = append(loggingProblems, err)
	}

	updateProblem := service.jobRunRepository.Update(run)

	runDto := buildJobRunDto(run)
	for _, loggingProblem := range loggingProblems {
		runDto.OutputExcerpt = appendBookkeepingProblem(runDto.OutputExcerpt, loggingProblem)
	}
	if updateProblem != nil {
		runDto.OutputExcerpt = appendBookkeepingProblem(runDto.OutputExcerpt, updateProblem)
	}

	return runDto, nil
}

// resolveLogFilePath 決定手動觸發的輸出要寫到哪個檔案。
//
// 優先寫回 job 自己的 log 目的地（foreign job 就是它的 redirect 目標）——使用者去
// 看那個檔案時，手動觸發的那一次也該在裡面。完全沒有目的地時退回我們的目錄，
// 至少讓這次執行看得到。
func (service *JobExecutionService) resolveLogFilePath(job *entity.CronJob) string {
	if logFilePath := job.ResolveLogFilePath(service.managedLogDirectory); logFilePath != "" {
		return logFilePath
	}

	return filepath.Join(service.managedLogDirectory, job.JobID()+".log")
}

// appendBookkeepingProblem 把我們自己的簿記錯誤附到輸出摘要末端。
//
// 不默默吞掉：使用者只會看輸出，把問題放在那裡才會被看到。也不讓它改變 job 的
// 成敗判定 —— 那是 job 自己的事。
func appendBookkeepingProblem(outputExcerpt string, problem error) string {
	notice := fmt.Sprintf("[crontab-watcher] %v", problem)

	if outputExcerpt == "" {
		return notice
	}

	if !strings.HasSuffix(outputExcerpt, "\n") {
		outputExcerpt += "\n"
	}

	return outputExcerpt + notice
}
