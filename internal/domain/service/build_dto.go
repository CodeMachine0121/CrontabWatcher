package service

import (
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

// entity → DTO 的對映集中在此。
//
// 這些是純粹的形狀轉換，不是領域計算（計算都掛在 entity 的 method 上），因此以
// package 私有函式呈現，供同 package 的各 service 共用。

func buildCronJobDto(
	job *entity.CronJob,
	latestRun *entity.JobRun,
	managedLogDirectory string,
	now time.Time,
) dto.CronJobDto {
	jobDto := dto.CronJobDto{
		JobID:                      job.JobID(),
		Origin:                     string(job.Origin()),
		Enabled:                    job.Enabled(),
		Description:                job.Description(),
		ScheduleExpression:         job.Schedule().Expression(),
		ScheduleOriginalExpression: job.Schedule().OriginalExpression(),
		ScheduleDescription:        job.Schedule().Describe(),
		Command:                    job.InnerCommand(),
		RawCommand:                 job.RawCommand(),
		LogSource:                  string(job.LogSource()),
		LogFilePath:                job.ResolveLogFilePath(managedLogDirectory),
		LineNumber:                 job.LineNumber(),
	}

	if nextRunAt, predictable := job.NextRunAt(now); predictable {
		jobDto.NextRunAt = &nextRunAt
		jobDto.NextRunPredictable = true
	}

	if latestRun != nil {
		latestRunDto := buildJobRunDto(latestRun)
		jobDto.LatestRun = &latestRunDto
	}

	return jobDto
}

func buildJobRunDto(run *entity.JobRun) dto.JobRunDto {
	runDto := dto.JobRunDto{
		RunID:           run.RunID(),
		JobID:           run.JobID(),
		TriggerSource:   string(run.TriggerSource()),
		RunStatus:       string(run.RunStatus()),
		StartedAt:       run.StartedAt(),
		OutputExcerpt:   run.OutputExcerpt(),
		OutputTruncated: run.OutputTruncated(),
	}

	if finishedAt, finished := run.FinishedAt(); finished {
		runDto.FinishedAt = &finishedAt
	}

	if duration, known := run.Duration(); known {
		durationMilliseconds := duration.Milliseconds()
		runDto.DurationMilliseconds = &durationMilliseconds
	}

	if exitCode, known := run.ExitCode(); known {
		runDto.ExitCode = &exitCode
	}

	return runDto
}

func buildJobRunDtos(runs []*entity.JobRun) []dto.JobRunDto {
	runDtos := make([]dto.JobRunDto, 0, len(runs))
	for _, run := range runs {
		runDtos = append(runDtos, buildJobRunDto(run))
	}

	return runDtos
}
