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
)

var taipeiLocation = mustLoadLocation("Asia/Taipei")

func mustLoadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return location
}

const (
	managedLogDirectory = "/data/logs"
	wrapperBinaryPath   = "/app/cronwatch"
)

var referenceNow = time.Date(2026, 8, 8, 1, 0, 0, 0, taipeiLocation)

type cronJobServiceFixture struct {
	service           *service.CronJobService
	crontabRepository *mocks.MockICrontabDocumentRepository
	jobRunRepository  *mocks.MockIJobRunRepository
}

func newCronJobServiceFixture(t *testing.T) cronJobServiceFixture {
	t.Helper()

	crontabRepository := mocks.NewMockICrontabDocumentRepository(t)
	jobRunRepository := mocks.NewMockIJobRunRepository(t)

	return cronJobServiceFixture{
		service:           service.NewCronJobService(crontabRepository, jobRunRepository, managedLogDirectory),
		crontabRepository: crontabRepository,
		jobRunRepository:  jobRunRepository,
	}
}

func (fixture cronJobServiceFixture) givenCrontab(content string) {
	fixture.crontabRepository.EXPECT().Load().
		Return(entity.ParseCrontabDocument(content), "fingerprint", nil)
}

func (fixture cronJobServiceFixture) givenNoRuns() {
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock_anySlice()).
		Return(map[string]*entity.JobRun{}, nil)
}

func TestListCronJobsReturnsEveryEntry(t *testing.T) {
	fixture := newCronJobServiceFixture(t)
	fixture.givenCrontab("" +
		"# a note\n" +
		"0 3 * * * /bin/first >> /var/log/first.log 2>&1\n" +
		"# cronwatch:id=job-managed\n" +
		"0 6 * * * /app/cronwatch run --job=job-managed -- /bin/second\n" +
		"#0 9 * * * /bin/third\n")
	fixture.givenNoRuns()

	jobs, err := fixture.service.ListCronJobs(referenceNow)

	require.NoError(t, err)
	require.Len(t, jobs, 3)

	assert.Equal(t, "foreign", jobs[0].Origin)
	assert.Equal(t, "redirect", jobs[0].LogSource)
	assert.Equal(t, "/var/log/first.log", jobs[0].LogFilePath)
	assert.Equal(t, "/bin/first", jobs[0].Command)
	assert.Equal(t, "/bin/first >> /var/log/first.log 2>&1", jobs[0].RawCommand)
	assert.Equal(t, 2, jobs[0].LineNumber)
	assert.True(t, jobs[0].Enabled)

	assert.Equal(t, "managed", jobs[1].Origin)
	assert.Equal(t, "job-managed", jobs[1].JobID)
	assert.Equal(t, "managed", jobs[1].LogSource)
	assert.Equal(t, "/data/logs/job-managed.log", jobs[1].LogFilePath)
	assert.Equal(t, "/bin/second", jobs[1].Command)

	assert.False(t, jobs[2].Enabled)
	assert.Nil(t, jobs[2].NextRunAt, "a disabled job will not run, so offering a next run time would mislead")
}

func TestListCronJobsOnAnEmptyCrontab(t *testing.T) {
	fixture := newCronJobServiceFixture(t)
	fixture.givenCrontab("")
	fixture.givenNoRuns()

	jobs, err := fixture.service.ListCronJobs(referenceNow)

	require.NoError(t, err)
	assert.Empty(t, jobs)
	assert.NotNil(t, jobs, "an empty result is an empty slice, not nil")
}

func TestListCronJobsComputesNextRunInTheGivenLocation(t *testing.T) {
	fixture := newCronJobServiceFixture(t)
	fixture.givenCrontab("0 3 * * * /bin/x\n")
	fixture.givenNoRuns()

	jobs, err := fixture.service.ListCronJobs(referenceNow)

	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.NotNil(t, jobs[0].NextRunAt)

	assert.True(t, jobs[0].NextRunPredictable)
	assert.Equal(t, "2026-08-08T03:00:00+08:00", jobs[0].NextRunAt.Format(time.RFC3339))
}

func TestListCronJobsLeavesRebootJobsWithoutANextRun(t *testing.T) {
	fixture := newCronJobServiceFixture(t)
	fixture.givenCrontab("@reboot /bin/warmup\n")
	fixture.givenNoRuns()

	jobs, err := fixture.service.ListCronJobs(referenceNow)

	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.False(t, jobs[0].NextRunPredictable)
	assert.Nil(t, jobs[0].NextRunAt)
}

func TestListCronJobsAttachesTheLatestRun(t *testing.T) {
	fixture := newCronJobServiceFixture(t)
	fixture.givenCrontab("# cronwatch:id=job-1\n0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n")

	latestRun := entity.NewJobRun("run-9", "job-1", entity.TriggerSourceSchedule, referenceNow.Add(-time.Hour))
	latestRun.Finish(referenceNow.Add(-time.Hour+2*time.Second), 0, false, "all good")

	fixture.jobRunRepository.EXPECT().LatestByJobIDs([]string{"job-1"}).
		Return(map[string]*entity.JobRun{"job-1": latestRun}, nil)

	jobs, err := fixture.service.ListCronJobs(referenceNow)

	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.NotNil(t, jobs[0].LatestRun)

	assert.Equal(t, "run-9", jobs[0].LatestRun.RunID)
	assert.Equal(t, "succeeded", jobs[0].LatestRun.RunStatus)
	assert.Equal(t, "all good", jobs[0].LatestRun.OutputExcerpt)
	require.NotNil(t, jobs[0].LatestRun.ExitCode)
	assert.Equal(t, 0, *jobs[0].LatestRun.ExitCode)
	require.NotNil(t, jobs[0].LatestRun.DurationMilliseconds)
	assert.Equal(t, int64(2000), *jobs[0].LatestRun.DurationMilliseconds)
}

func TestListCronJobsLeavesLatestRunNilWhenThereAreNoRecords(t *testing.T) {
	fixture := newCronJobServiceFixture(t)
	fixture.givenCrontab("0 3 * * * /bin/x\n")
	fixture.givenNoRuns()

	jobs, err := fixture.service.ListCronJobs(referenceNow)

	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Nil(t, jobs[0].LatestRun)
}

func TestListCronJobsPropagatesACrontabReadFailure(t *testing.T) {
	fixture := newCronJobServiceFixture(t)
	readFailure := errors.New("permission denied")
	fixture.crontabRepository.EXPECT().Load().Return(nil, "", readFailure)

	_, err := fixture.service.ListCronJobs(referenceNow)

	assert.ErrorIs(t, err, readFailure)
}

func TestListCronJobsPropagatesARunHistoryReadFailure(t *testing.T) {
	// 刻意不降級成「沒有紀錄」——那會讓一個實際上跑失敗的 job 在頁面上看起來
	// 只是「還沒跑過」。
	fixture := newCronJobServiceFixture(t)
	fixture.givenCrontab("0 3 * * * /bin/x\n")

	readFailure := errors.New("corrupt run record file")
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock_anySlice()).Return(nil, readFailure)

	_, err := fixture.service.ListCronJobs(referenceNow)

	assert.ErrorIs(t, err, readFailure)
}

func TestGetCronJob(t *testing.T) {
	fixture := newCronJobServiceFixture(t)
	fixture.givenCrontab("# cronwatch:id=job-1\n0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n")
	fixture.jobRunRepository.EXPECT().LatestByJobIDs([]string{"job-1"}).
		Return(map[string]*entity.JobRun{}, nil)

	job, err := fixture.service.GetCronJob("job-1", referenceNow)

	require.NoError(t, err)
	assert.Equal(t, "job-1", job.JobID)
	assert.Equal(t, "/bin/x", job.Command)
}

func TestGetCronJobRejectsAnUnknownIdentifier(t *testing.T) {
	fixture := newCronJobServiceFixture(t)
	fixture.givenCrontab("0 3 * * * /bin/x\n")

	_, err := fixture.service.GetCronJob("does-not-exist", referenceNow)

	assert.ErrorIs(t, err, entity.ErrCronJobNotFound)
}

func TestListCronJobsDescribesTheSchedule(t *testing.T) {
	fixture := newCronJobServiceFixture(t)
	fixture.givenCrontab("@daily /bin/x\n")
	fixture.givenNoRuns()

	jobs, err := fixture.service.ListCronJobs(referenceNow)

	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, "0 0 * * *", jobs[0].ScheduleExpression)
	assert.Equal(t, "@daily", jobs[0].ScheduleOriginalExpression)
	assert.Equal(t, "每天 00:00", jobs[0].ScheduleDescription)
}
