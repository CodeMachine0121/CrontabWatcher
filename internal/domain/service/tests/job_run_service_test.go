package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/domain/interface/mocks"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

type jobRunServiceFixture struct {
	service           *service.JobRunService
	crontabRepository *mocks.MockICrontabDocumentRepository
	jobRunRepository  *mocks.MockIJobRunRepository
	jobLogRepository  *mocks.MockIJobLogRepository
}

func newJobRunServiceFixture(t *testing.T) jobRunServiceFixture {
	t.Helper()

	crontabRepository := mocks.NewMockICrontabDocumentRepository(t)
	jobRunRepository := mocks.NewMockIJobRunRepository(t)
	jobLogRepository := mocks.NewMockIJobLogRepository(t)

	return jobRunServiceFixture{
		service: service.NewJobRunService(
			crontabRepository, jobRunRepository, jobLogRepository, managedLogDirectory),
		crontabRepository: crontabRepository,
		jobRunRepository:  jobRunRepository,
		jobLogRepository:  jobLogRepository,
	}
}

func (fixture jobRunServiceFixture) givenCrontab(content string) {
	fixture.crontabRepository.EXPECT().Load().
		Return(entity.ParseCrontabDocument(content), "fingerprint", nil)
}

const managedJobCrontab = "# cronwatch:id=job-1\n0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n"

func TestListJobRunsReturnsHistoryNewestFirst(t *testing.T) {
	fixture := newJobRunServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)

	firstRun := entity.NewJobRun("run-1", "job-1", entity.TriggerSourceSchedule, referenceNow.Add(-2*time.Hour))
	firstRun.Finish(referenceNow.Add(-2*time.Hour+time.Second), 0, false, "ok")
	secondRun := entity.NewJobRun("run-2", "job-1", entity.TriggerSourceManual, referenceNow.Add(-time.Hour))
	secondRun.Finish(referenceNow.Add(-time.Hour+time.Second), 3, false, "boom")

	fixture.jobRunRepository.EXPECT().ListByJobID("job-1", 10).
		Return([]*entity.JobRun{secondRun, firstRun}, nil)

	runList, err := fixture.service.ListJobRuns("job-1", 10)

	require.NoError(t, err)
	assert.Equal(t, "job-1", runList.JobID)
	assert.Empty(t, runList.UnavailableReason)
	require.Len(t, runList.Runs, 2)

	assert.Equal(t, "run-2", runList.Runs[0].RunID)
	assert.Equal(t, "manual", runList.Runs[0].TriggerSource)
	assert.Equal(t, "failed", runList.Runs[0].RunStatus)
	require.NotNil(t, runList.Runs[0].ExitCode)
	assert.Equal(t, 3, *runList.Runs[0].ExitCode)
}

func TestListJobRunsExplainsWhyAForeignJobHasNoHistory(t *testing.T) {
	// 空陣列加上原因，遠好過讓使用者對著空表格猜「是沒跑過還是壞了」。
	fixture := newJobRunServiceFixture(t)
	fixture.givenCrontab("0 3 * * * /bin/x >> /var/log/x.log\n")

	document := entity.ParseCrontabDocument("0 3 * * * /bin/x >> /var/log/x.log\n")
	foreignJobID := document.Jobs()[0].JobID()

	runList, err := fixture.service.ListJobRuns(foreignJobID, 10)

	require.NoError(t, err)
	assert.Empty(t, runList.Runs)
	assert.NotNil(t, runList.Runs, "an empty history is an empty slice, not nil")
	assert.Contains(t, runList.UnavailableReason, "not managed")
}

func TestListJobRunsRejectsAnUnknownJob(t *testing.T) {
	fixture := newJobRunServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)

	_, err := fixture.service.ListJobRuns("does-not-exist", 10)

	assert.ErrorIs(t, err, entity.ErrCronJobNotFound)
}

func TestListJobRunsPropagatesAReadFailure(t *testing.T) {
	fixture := newJobRunServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)

	readFailure := errors.New("unreadable run record file")
	fixture.jobRunRepository.EXPECT().ListByJobID("job-1", 10).Return(nil, readFailure)

	_, err := fixture.service.ListJobRuns("job-1", 10)

	assert.ErrorIs(t, err, readFailure)
}

func TestTailJobLogReadsTheManagedLogFile(t *testing.T) {
	fixture := newJobRunServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)

	fixture.jobLogRepository.EXPECT().Tail("/data/logs/job-1.log", 50).
		Return(vo.NewJobLogTail("line one\nline two\n", true, false), nil)

	logDto, err := fixture.service.TailJobLog("job-1", 50)

	require.NoError(t, err)
	assert.Equal(t, "job-1", logDto.JobID)
	assert.Equal(t, "managed", logDto.LogSource)
	assert.Equal(t, "/data/logs/job-1.log", logDto.FilePath)
	assert.True(t, logDto.Exists)
	assert.False(t, logDto.Truncated)
	assert.Equal(t, 2, logDto.LineCount)
	assert.Equal(t, "line one\nline two\n", logDto.Content)
}

func TestTailJobLogReadsTheRedirectTargetOfAForeignJob(t *testing.T) {
	crontabContent := "0 3 * * * /bin/x >> /var/log/x.log 2>&1\n"
	fixture := newJobRunServiceFixture(t)
	fixture.givenCrontab(crontabContent)

	foreignJobID := entity.ParseCrontabDocument(crontabContent).Jobs()[0].JobID()

	fixture.jobLogRepository.EXPECT().Tail("/var/log/x.log", 50).
		Return(vo.NewJobLogTail("from the user's own log\n", true, false), nil)

	logDto, err := fixture.service.TailJobLog(foreignJobID, 50)

	require.NoError(t, err)
	assert.Equal(t, "redirect", logDto.LogSource)
	assert.Equal(t, "/var/log/x.log", logDto.FilePath)
	assert.Equal(t, "from the user's own log\n", logDto.Content)
}

func TestTailJobLogRefusesWhenThereIsNoLogToRead(t *testing.T) {
	// 回 200 空字串會被讀成「跑過但沒輸出」。與「根本無從得知」是完全不同的事實，
	// 所以這裡必須是一個明確的錯誤。
	testCases := []struct {
		name    string
		crontab string
	}{
		{name: "no redirect at all", crontab: "0 3 * * * /bin/x\n"},
		{name: "output discarded to /dev/null", crontab: "0 3 * * * /bin/x > /dev/null 2>&1\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newJobRunServiceFixture(t)
			fixture.givenCrontab(testCase.crontab)

			jobID := entity.ParseCrontabDocument(testCase.crontab).Jobs()[0].JobID()

			_, err := fixture.service.TailJobLog(jobID, 50)

			assert.ErrorIs(t, err, entity.ErrJobLogUnavailable)
		})
	}
}

func TestTailJobLogOnAJobThatHasNotRunYet(t *testing.T) {
	fixture := newJobRunServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)

	fixture.jobLogRepository.EXPECT().Tail("/data/logs/job-1.log", 50).
		Return(vo.NewMissingJobLogTail(), nil)

	logDto, err := fixture.service.TailJobLog("job-1", 50)

	require.NoError(t, err, "a log file that does not exist yet is a normal state, not an error")
	assert.False(t, logDto.Exists)
	assert.Empty(t, logDto.Content)
	assert.Zero(t, logDto.LineCount)
}

func TestTailJobLogRejectsAnUnknownJob(t *testing.T) {
	fixture := newJobRunServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)

	_, err := fixture.service.TailJobLog("does-not-exist", 50)

	assert.ErrorIs(t, err, entity.ErrCronJobNotFound)
}

func TestTailJobLogPropagatesAReadFailure(t *testing.T) {
	fixture := newJobRunServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)

	readFailure := errors.New("permission denied")
	fixture.jobLogRepository.EXPECT().Tail("/data/logs/job-1.log", 50).
		Return(vo.JobLogTail{}, readFailure)

	_, err := fixture.service.TailJobLog("job-1", 50)

	assert.ErrorIs(t, err, readFailure)
}

func TestTailJobLogReportsTruncation(t *testing.T) {
	fixture := newJobRunServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)

	fixture.jobLogRepository.EXPECT().Tail("/data/logs/job-1.log", 50).
		Return(vo.NewJobLogTail("partial\n", true, true), nil)

	logDto, err := fixture.service.TailJobLog("job-1", 50)

	require.NoError(t, err)
	assert.True(t, logDto.Truncated, "the UI has to be able to say the view is partial")
}
