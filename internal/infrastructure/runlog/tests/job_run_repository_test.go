package runlog_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/runlog"
)

var baseTime = time.Date(2026, 8, 8, 14, 0, 0, 0, time.UTC)

const defaultRetentionCount = 500

func newJobRunRepository(t *testing.T, retentionCount int) (*runlog.JobRunRepository, string) {
	t.Helper()

	recordFilePath := filepath.Join(t.TempDir(), "state", "runs.jsonl")

	return runlog.NewJobRunRepository(recordFilePath, retentionCount), recordFilePath
}

func appendFinishedRun(t *testing.T, repository *runlog.JobRunRepository, runID string, jobID string, offset time.Duration, exitCode int) *entity.JobRun {
	t.Helper()

	run := entity.NewJobRun(runID, jobID, entity.TriggerSourceSchedule, baseTime.Add(offset))
	require.NoError(t, repository.Append(run))

	run.Finish(baseTime.Add(offset+time.Second), exitCode, false, "output of "+runID)
	require.NoError(t, repository.Update(run))

	return run
}

func TestAppendCreatesTheFileAndItsParentDirectory(t *testing.T) {
	repository, recordFilePath := newJobRunRepository(t, defaultRetentionCount)

	run := entity.NewJobRun("run-1", "job-1", entity.TriggerSourceManual, baseTime)
	require.NoError(t, repository.Append(run))

	contentBytes, err := os.ReadFile(recordFilePath)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(contentBytes), "\n"), "one record is one line")
	assert.Contains(t, string(contentBytes), `"runId":"run-1"`)
}

func TestAppendWritesOneLinePerRecord(t *testing.T) {
	repository, recordFilePath := newJobRunRepository(t, defaultRetentionCount)

	require.NoError(t, repository.Append(entity.NewJobRun("run-1", "job-1", entity.TriggerSourceManual, baseTime)))
	require.NoError(t, repository.Append(entity.NewJobRun("run-2", "job-1", entity.TriggerSourceManual, baseTime.Add(time.Minute))))

	contentBytes, err := os.ReadFile(recordFilePath)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(contentBytes), "\n"))
}

func TestUpdateReplacesOneRecordAndLeavesTheOthersAlone(t *testing.T) {
	repository, recordFilePath := newJobRunRepository(t, defaultRetentionCount)

	firstRun := entity.NewJobRun("run-1", "job-1", entity.TriggerSourceManual, baseTime)
	require.NoError(t, repository.Append(firstRun))
	require.NoError(t, repository.Append(entity.NewJobRun("run-2", "job-1", entity.TriggerSourceManual, baseTime.Add(time.Minute))))

	firstRun.Finish(baseTime.Add(2*time.Second), 0, false, "done")
	require.NoError(t, repository.Update(firstRun))

	contentBytes, err := os.ReadFile(recordFilePath)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(contentBytes), "\n"), "updating must not add a line")

	runs, err := repository.ListByJobID("job-1", 0)
	require.NoError(t, err)
	require.Len(t, runs, 2)

	updatedRun := findRun(t, runs, "run-1")
	assert.Equal(t, entity.RunStatusSucceeded, updatedRun.RunStatus())
	assert.Equal(t, "done", updatedRun.OutputExcerpt())

	untouchedRun := findRun(t, runs, "run-2")
	assert.Equal(t, entity.RunStatusRunning, untouchedRun.RunStatus())
}

func TestUpdateRejectsAnUnknownRun(t *testing.T) {
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	unknownRun := entity.NewJobRun("run-missing", "job-1", entity.TriggerSourceManual, baseTime)

	assert.ErrorIs(t, repository.Update(unknownRun), entity.ErrJobRunNotFound)
}

func TestListByJobIDReturnsNewestFirst(t *testing.T) {
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	appendFinishedRun(t, repository, "run-1", "job-1", 0, 0)
	appendFinishedRun(t, repository, "run-2", "job-1", time.Minute, 0)
	appendFinishedRun(t, repository, "run-3", "job-1", 2*time.Minute, 1)

	runs, err := repository.ListByJobID("job-1", 0)
	require.NoError(t, err)
	require.Len(t, runs, 3)

	assert.Equal(t, "run-3", runs[0].RunID())
	assert.Equal(t, "run-2", runs[1].RunID())
	assert.Equal(t, "run-1", runs[2].RunID())
}

func TestListByJobIDHonoursTheLimit(t *testing.T) {
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	appendFinishedRun(t, repository, "run-1", "job-1", 0, 0)
	appendFinishedRun(t, repository, "run-2", "job-1", time.Minute, 0)
	appendFinishedRun(t, repository, "run-3", "job-1", 2*time.Minute, 0)

	runs, err := repository.ListByJobID("job-1", 2)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, "run-3", runs[0].RunID())
	assert.Equal(t, "run-2", runs[1].RunID())
}

func TestListByJobIDFiltersByJob(t *testing.T) {
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	appendFinishedRun(t, repository, "run-1", "job-1", 0, 0)
	appendFinishedRun(t, repository, "run-2", "job-2", time.Minute, 0)

	runs, err := repository.ListByJobID("job-1", 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "run-1", runs[0].RunID())
}

func TestListByJobIDOnAMissingFile(t *testing.T) {
	// 還沒有任何 job 跑過。空清單，不是錯誤。
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	runs, err := repository.ListByJobID("job-1", 0)

	require.NoError(t, err)
	assert.Empty(t, runs)
	assert.NotNil(t, runs, "an empty result is an empty slice, not nil")
}

func TestListByJobIDSkipsCorruptLines(t *testing.T) {
	// 一行壞掉不該讓整份歷史消失。一次沒寫完的 append 或人為編輯都可能造成
	// 這種情況，而其餘紀錄仍然完好。
	repository, recordFilePath := newJobRunRepository(t, defaultRetentionCount)

	appendFinishedRun(t, repository, "run-1", "job-1", 0, 0)

	file, err := os.OpenFile(recordFilePath, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = file.WriteString("{not valid json\n")
	require.NoError(t, err)
	require.NoError(t, file.Close())

	appendFinishedRun(t, repository, "run-2", "job-1", time.Minute, 0)

	runs, err := repository.ListByJobID("job-1", 0)
	require.NoError(t, err)
	assert.Len(t, runs, 2, "the two intact records survive the broken line between them")
}

func TestLatestByJobIDs(t *testing.T) {
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	appendFinishedRun(t, repository, "run-1", "job-1", 0, 0)
	appendFinishedRun(t, repository, "run-2", "job-1", 2*time.Minute, 1)
	appendFinishedRun(t, repository, "run-3", "job-2", time.Minute, 0)

	latestRuns, err := repository.LatestByJobIDs([]string{"job-1", "job-2", "job-without-runs"})
	require.NoError(t, err)

	require.Contains(t, latestRuns, "job-1")
	assert.Equal(t, "run-2", latestRuns["job-1"].RunID())

	require.Contains(t, latestRuns, "job-2")
	assert.Equal(t, "run-3", latestRuns["job-2"].RunID())

	assert.NotContains(t, latestRuns, "job-without-runs",
		"a job with no runs is absent from the map rather than mapped to nil")
}

func TestLatestByJobIDsOnAMissingFile(t *testing.T) {
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	latestRuns, err := repository.LatestByJobIDs([]string{"job-1"})

	require.NoError(t, err)
	assert.Empty(t, latestRuns)
}

func TestHasRunningRun(t *testing.T) {
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	appendFinishedRun(t, repository, "run-1", "job-1", 0, 0)

	hasRunning, err := repository.HasRunningRun("job-1")
	require.NoError(t, err)
	assert.False(t, hasRunning, "a finished run does not count as running")

	require.NoError(t, repository.Append(
		entity.NewJobRun("run-2", "job-1", entity.TriggerSourceManual, baseTime.Add(time.Minute))))

	hasRunning, err = repository.HasRunningRun("job-1")
	require.NoError(t, err)
	assert.True(t, hasRunning)

	otherJobHasRunning, err := repository.HasRunningRun("job-2")
	require.NoError(t, err)
	assert.False(t, otherJobHasRunning, "running state is per job")
}

func TestMarkRunningRunsAsInterrupted(t *testing.T) {
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	appendFinishedRun(t, repository, "run-finished", "job-1", 0, 0)
	require.NoError(t, repository.Append(
		entity.NewJobRun("run-orphan-1", "job-1", entity.TriggerSourceManual, baseTime.Add(time.Minute))))
	require.NoError(t, repository.Append(
		entity.NewJobRun("run-orphan-2", "job-2", entity.TriggerSourceSchedule, baseTime.Add(2*time.Minute))))

	interruptedCount, err := repository.MarkRunningRunsAsInterrupted(baseTime.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, interruptedCount)

	firstJobRuns, err := repository.ListByJobID("job-1", 0)
	require.NoError(t, err)

	orphanRun := findRun(t, firstJobRuns, "run-orphan-1")
	assert.Equal(t, entity.RunStatusUnknown, orphanRun.RunStatus())
	assert.Contains(t, orphanRun.OutputExcerpt(), "interrupted by restart")

	finishedRun := findRun(t, firstJobRuns, "run-finished")
	assert.Equal(t, entity.RunStatusSucceeded, finishedRun.RunStatus(), "finished runs are left alone")

	secondPassCount, err := repository.MarkRunningRunsAsInterrupted(baseTime.Add(2 * time.Hour))
	require.NoError(t, err)
	assert.Zero(t, secondPassCount, "nothing left to interrupt on a second pass")
}

func TestMarkRunningRunsAsInterruptedOnAMissingFile(t *testing.T) {
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	interruptedCount, err := repository.MarkRunningRunsAsInterrupted(baseTime)

	require.NoError(t, err)
	assert.Zero(t, interruptedCount)
}

func TestAppendCompactsBeyondTheRetentionCount(t *testing.T) {
	const retentionCount = 3
	repository, _ := newJobRunRepository(t, retentionCount)

	for runIndex := 0; runIndex < 6; runIndex++ {
		appendFinishedRun(t, repository, "run-"+string(rune('a'+runIndex)), "job-1", time.Duration(runIndex)*time.Minute, 0)
	}

	runs, err := repository.ListByJobID("job-1", 0)
	require.NoError(t, err)
	require.Len(t, runs, retentionCount, "only the newest records per job are kept")

	assert.Equal(t, "run-f", runs[0].RunID())
	assert.Equal(t, "run-e", runs[1].RunID())
	assert.Equal(t, "run-d", runs[2].RunID())
}

func TestRetentionIsPerJob(t *testing.T) {
	const retentionCount = 2
	repository, _ := newJobRunRepository(t, retentionCount)

	appendFinishedRun(t, repository, "run-a1", "job-1", 0, 0)
	appendFinishedRun(t, repository, "run-a2", "job-1", time.Minute, 0)
	appendFinishedRun(t, repository, "run-a3", "job-1", 2*time.Minute, 0)
	appendFinishedRun(t, repository, "run-b1", "job-2", 3*time.Minute, 0)

	firstJobRuns, err := repository.ListByJobID("job-1", 0)
	require.NoError(t, err)
	assert.Len(t, firstJobRuns, retentionCount)

	secondJobRuns, err := repository.ListByJobID("job-2", 0)
	require.NoError(t, err)
	assert.Len(t, secondJobRuns, 1, "one job hitting its cap must not evict another job's records")
}

func TestConcurrentAppendsProduceIntactLines(t *testing.T) {
	repository, _ := newJobRunRepository(t, defaultRetentionCount)

	const concurrentWriterCount = 16
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrentWriterCount)

	for writerIndex := 0; writerIndex < concurrentWriterCount; writerIndex++ {
		go func(index int) {
			defer waitGroup.Done()

			run := entity.NewJobRun(
				"run-"+string(rune('a'+index)), "job-1", entity.TriggerSourceManual,
				baseTime.Add(time.Duration(index)*time.Second))
			_ = repository.Append(run)
		}(writerIndex)
	}

	waitGroup.Wait()

	runs, err := repository.ListByJobID("job-1", 0)
	require.NoError(t, err)
	assert.Len(t, runs, concurrentWriterCount, "no record is lost and no line is mangled")
}

func findRun(t *testing.T, runs []*entity.JobRun, runID string) *entity.JobRun {
	t.Helper()

	for _, run := range runs {
		if run.RunID() == runID {
			return run
		}
	}

	t.Fatalf("run %s not found", runID)

	return nil
}
