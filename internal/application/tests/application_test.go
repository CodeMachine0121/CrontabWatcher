package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/application"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/domain/interface/mocks"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

// 這一層的測試注入**真實的 domain service 與 entity**，只 mock 最外層的
// repository/proxy。因此測 application 時會連帶測到 service 與 entity，
// 這是刻意的測試力度放大。

const (
	managedLogDirectory    = "/data/logs"
	wrapperBinaryPath      = "/app/cronwatch"
	defaultLogTailLines    = 200
	manualTriggerTimeout   = 15 * time.Minute
	managedJobCrontab      = "# cronwatch:id=job-1\n0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n"
	foreignRedirectCrontab = "0 3 * * * /bin/y >> /var/log/y.log 2>&1\n"
)

var (
	taipeiLocation = mustLoadLocation("Asia/Taipei")
	referenceNow   = time.Date(2026, 8, 8, 1, 0, 0, 0, taipeiLocation)
)

func mustLoadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return location
}

type applicationFixture struct {
	cronJobApplication       *application.CronJobApplication
	jobRunApplication        *application.JobRunApplication
	manualTriggerApplication *application.ManualTriggerApplication
	crontabEditApplication   *application.CrontabEditApplication

	crontabRepository *mocks.MockICrontabDocumentRepository
	jobRunRepository  *mocks.MockIJobRunRepository
	jobLogRepository  *mocks.MockIJobLogRepository
	commandProxy      *mocks.MockICommandExecutionProxy
	identifiers       *mocks.MockIIdentifierGenerator
	clock             *mocks.MockIClock
}

type fixtureOptions struct {
	manualTriggerEnabled bool
	crontabWriteEnabled  bool
}

func newApplicationFixture(t *testing.T, options fixtureOptions) applicationFixture {
	t.Helper()

	crontabRepository := mocks.NewMockICrontabDocumentRepository(t)
	jobRunRepository := mocks.NewMockIJobRunRepository(t)
	jobLogRepository := mocks.NewMockIJobLogRepository(t)
	commandProxy := mocks.NewMockICommandExecutionProxy(t)
	identifiers := mocks.NewMockIIdentifierGenerator(t)
	clock := mocks.NewMockIClock(t)

	cronJobService := service.NewCronJobService(crontabRepository, jobRunRepository, managedLogDirectory)
	jobRunService := service.NewJobRunService(crontabRepository, jobRunRepository, jobLogRepository, managedLogDirectory)
	jobExecutionService := service.NewJobExecutionService(
		crontabRepository, jobRunRepository, jobLogRepository,
		commandProxy, identifiers, clock, managedLogDirectory)
	crontabEditService := service.NewCrontabEditService(
		crontabRepository, identifiers, wrapperBinaryPath, managedLogDirectory)

	return applicationFixture{
		cronJobApplication: application.NewCronJobApplication(cronJobService, clock),
		jobRunApplication:  application.NewJobRunApplication(jobRunService, defaultLogTailLines),
		manualTriggerApplication: application.NewManualTriggerApplication(
			jobExecutionService, options.manualTriggerEnabled, manualTriggerTimeout),
		crontabEditApplication: application.NewCrontabEditApplication(
			crontabEditService, clock, options.crontabWriteEnabled),

		crontabRepository: crontabRepository,
		jobRunRepository:  jobRunRepository,
		jobLogRepository:  jobLogRepository,
		commandProxy:      commandProxy,
		identifiers:       identifiers,
		clock:             clock,
	}
}

func newReadOnlyFixture(t *testing.T) applicationFixture {
	return newApplicationFixture(t, fixtureOptions{})
}

func newWritableFixture(t *testing.T) applicationFixture {
	return newApplicationFixture(t, fixtureOptions{manualTriggerEnabled: true, crontabWriteEnabled: true})
}

func (fixture applicationFixture) givenCrontab(content string) {
	fixture.crontabRepository.EXPECT().Load().
		Return(entity.ParseCrontabDocument(content), "fingerprint-1", nil)
}

func TestListCronJobsWalksTheWholeStack(t *testing.T) {
	// 真實 crontab 文字進去，完整 DTO 出來 —— 這一個測試同時覆蓋 parse、
	// entity 計算、service 轉換與 application 編排。
	fixture := newReadOnlyFixture(t)
	fixture.givenCrontab("" +
		"# a note\n" +
		"SHELL=/bin/sh\n" +
		managedJobCrontab +
		foreignRedirectCrontab +
		"#0 9 * * * /bin/disabled\n" +
		"@reboot /bin/warmup\n")
	fixture.clock.EXPECT().Now().Return(referenceNow)
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock.Anything).
		Return(map[string]*entity.JobRun{}, nil)

	jobs, err := fixture.cronJobApplication.ListCronJobs()

	require.NoError(t, err)
	require.Len(t, jobs, 4)

	assert.Equal(t, "job-1", jobs[0].JobID)
	assert.Equal(t, "managed", jobs[0].Origin)
	assert.Equal(t, "/bin/x", jobs[0].Command)
	assert.Equal(t, "/data/logs/job-1.log", jobs[0].LogFilePath)
	require.NotNil(t, jobs[0].NextRunAt)
	assert.Equal(t, "2026-08-08T03:00:00+08:00", jobs[0].NextRunAt.Format(time.RFC3339),
		"the configured timezone must survive all the way out to the dto")

	assert.Equal(t, "foreign", jobs[1].Origin)
	assert.Equal(t, "/var/log/y.log", jobs[1].LogFilePath)

	assert.False(t, jobs[2].Enabled)
	assert.Nil(t, jobs[2].NextRunAt)

	assert.False(t, jobs[3].NextRunPredictable)
}

func TestGetCronJobRejectsAnUnknownJob(t *testing.T) {
	fixture := newReadOnlyFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.clock.EXPECT().Now().Return(referenceNow)

	_, err := fixture.cronJobApplication.GetCronJob("does-not-exist")

	assert.ErrorIs(t, err, entity.ErrCronJobNotFound)
}

func TestTailJobLogUsesTheConfiguredDefaultLineCount(t *testing.T) {
	fixture := newReadOnlyFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.jobLogRepository.EXPECT().Tail("/data/logs/job-1.log", defaultLogTailLines).
		Return(vo.NewJobLogTail("content\n", true, false), nil)

	logDto, err := fixture.jobRunApplication.TailJobLog("job-1", 0)

	require.NoError(t, err)
	assert.Equal(t, "content\n", logDto.Content)
}

func TestTailJobLogClampsAnExcessiveLineCount(t *testing.T) {
	// 要一萬行就給五千行。為此回 400 只是把一個能滿足的請求變成失敗。
	fixture := newReadOnlyFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.jobLogRepository.EXPECT().Tail("/data/logs/job-1.log", application.MaximumLogTailLines).
		Return(vo.NewJobLogTail("content\n", true, false), nil)

	_, err := fixture.jobRunApplication.TailJobLog("job-1", 10_000)

	require.NoError(t, err)
}

func TestTailJobLogRefusesAJobWithNoReadableOutput(t *testing.T) {
	fixture := newReadOnlyFixture(t)
	fixture.givenCrontab("0 3 * * * /bin/z\n")

	jobID := entity.ParseCrontabDocument("0 3 * * * /bin/z\n").Jobs()[0].JobID()

	_, err := fixture.jobRunApplication.TailJobLog(jobID, 0)

	assert.ErrorIs(t, err, entity.ErrJobLogUnavailable)
}

func TestListJobRunsClampsTheLimit(t *testing.T) {
	fixture := newReadOnlyFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.jobRunRepository.EXPECT().ListByJobID("job-1", application.MaximumJobRunHistoryLimit).
		Return([]*entity.JobRun{}, nil)

	runList, err := fixture.jobRunApplication.ListJobRuns("job-1", 99_999)

	require.NoError(t, err)
	assert.Empty(t, runList.Runs)
}

func TestTriggerJobRunIsRefusedWhenDisabled(t *testing.T) {
	// 開關關著時連 crontab 都不該被讀 —— mock 未設定 Load，被呼叫就會失敗。
	fixture := newReadOnlyFixture(t)

	_, err := fixture.manualTriggerApplication.TriggerJobRun(context.Background(), "job-1")

	assert.ErrorIs(t, err, application.ErrManualTriggerDisabled)
}

func TestTriggerJobRunAppliesTheConfiguredTimeout(t *testing.T) {
	fixture := newWritableFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.jobRunRepository.EXPECT().HasRunningRun("job-1").Return(false, nil)
	fixture.identifiers.EXPECT().NewIdentifier().Return("run-1")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)
	fixture.jobLogRepository.EXPECT().Append(mock.Anything, mock.Anything).Return(nil)

	fixture.commandProxy.EXPECT().Execute(mock.Anything, "/bin/x", manualTriggerTimeout).
		Return(vo.NewCommandExecutionResult("done\n", 0, false), nil)

	runDto, err := fixture.manualTriggerApplication.TriggerJobRun(context.Background(), "job-1")

	require.NoError(t, err)
	assert.Equal(t, "succeeded", runDto.RunStatus)
	assert.Equal(t, "manual", runDto.TriggerSource)
}

func TestTriggerJobRunSurvivesTheRequestBeingCancelled(t *testing.T) {
	// 使用者關掉分頁不該把備份腳本砍到一半。application 剝掉請求 context 的
	// 取消訊號，只留下我們自己設的逾時。
	fixture := newWritableFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.jobRunRepository.EXPECT().HasRunningRun("job-1").Return(false, nil)
	fixture.identifiers.EXPECT().NewIdentifier().Return("run-1")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)
	fixture.jobLogRepository.EXPECT().Append(mock.Anything, mock.Anything).Return(nil)

	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()

	fixture.commandProxy.EXPECT().
		Execute(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil }),
			"/bin/x", manualTriggerTimeout).
		Return(vo.NewCommandExecutionResult("done\n", 0, false), nil)

	runDto, err := fixture.manualTriggerApplication.TriggerJobRun(cancelledContext, "job-1")

	require.NoError(t, err)
	assert.Equal(t, "succeeded", runDto.RunStatus)
}

func TestRecordWrapperRunIgnoresTheManualTriggerSwitch(t *testing.T) {
	// 那個開關管的是瀏覽器上的按鈕，不是 cron 的排程。讓它擋住 wrapper 會直接
	// 讓使用者的 job 不執行。
	fixture := newReadOnlyFixture(t)

	fixture.identifiers.EXPECT().NewIdentifier().Return("run-1")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)
	fixture.jobLogRepository.EXPECT().Append(mock.Anything, mock.Anything).Return(nil)
	fixture.commandProxy.EXPECT().Execute(mock.Anything, "/bin/backup.sh", time.Duration(0)).
		Return(vo.NewCommandExecutionResult("ok\n", 0, false), nil)

	runDto, err := fixture.manualTriggerApplication.RecordWrapperRun(
		context.Background(), "job-1", "/bin/backup.sh")

	require.NoError(t, err)
	assert.Equal(t, "schedule", runDto.TriggerSource)
}

func TestWriteUseCasesAreRefusedInReadOnlyMode(t *testing.T) {
	// 每個寫入用例都要各自擋下來，而且擋在讀檔之前 —— mock 未設定任何期望。
	fixture := newReadOnlyFixture(t)

	_, createErr := fixture.crontabEditApplication.CreateCronJob("0 3 * * *", "/bin/x", "", true)
	assert.ErrorIs(t, createErr, application.ErrCrontabWriteDisabled)

	_, updateErr := fixture.crontabEditApplication.UpdateCronJob("job-1", "0 3 * * *", "/bin/x", "", true)
	assert.ErrorIs(t, updateErr, application.ErrCrontabWriteDisabled)

	deleteErr := fixture.crontabEditApplication.DeleteCronJob("job-1")
	assert.ErrorIs(t, deleteErr, application.ErrCrontabWriteDisabled)

	_, enableErr := fixture.crontabEditApplication.SetCronJobEnabled("job-1", false)
	assert.ErrorIs(t, enableErr, application.ErrCrontabWriteDisabled)

	_, adoptErr := fixture.crontabEditApplication.AdoptCronJob("job-1")
	assert.ErrorIs(t, adoptErr, application.ErrCrontabWriteDisabled)

	assert.False(t, fixture.crontabEditApplication.WriteEnabled())
}

func TestReadingTheCrontabIsAllowedInReadOnlyMode(t *testing.T) {
	crontabContent := "# a note\n0 3 * * * /bin/x\n"
	fixture := newReadOnlyFixture(t)
	fixture.givenCrontab(crontabContent)

	content, err := fixture.crontabEditApplication.GetCrontabContent()

	require.NoError(t, err)
	assert.Equal(t, crontabContent, content)
}

func TestCreateCronJobWalksTheWholeStack(t *testing.T) {
	fixture := newWritableFixture(t)
	fixture.givenCrontab("# existing\n0 9 * * * /bin/existing\n")
	fixture.identifiers.EXPECT().NewIdentifier().Return("job-new")
	fixture.clock.EXPECT().Now().Return(referenceNow)

	savedContent := ""
	fixture.crontabRepository.EXPECT().Save(mock.Anything, "fingerprint-1").
		Run(func(document *entity.CrontabDocument, expectedFingerprint string) {
			savedContent = document.Render()
		}).Return(nil)

	jobDto, err := fixture.crontabEditApplication.CreateCronJob("0 3 * * *", "/bin/backup.sh", "nightly", true)

	require.NoError(t, err)
	assert.Equal(t, "job-new", jobDto.JobID)
	assert.Equal(t, "managed", jobDto.Origin)

	assert.Equal(t,
		"# existing\n"+
			"0 9 * * * /bin/existing\n"+
			"# nightly\n"+
			"# cronwatch:id=job-new\n"+
			"0 3 * * * /app/cronwatch run --job=job-new -- /bin/backup.sh\n",
		savedContent,
		"the pre-existing lines must come through byte for byte")
}

func TestCreateCronJobRejectsAnInvalidScheduleBeforeTouchingTheFile(t *testing.T) {
	// 重點是檔案完全沒被碰到：Load 與 Save 都沒有設定期望，被呼叫就會失敗。
	// 時鐘允許被問（它不是 I/O，只是個引數）。
	fixture := newWritableFixture(t)
	fixture.clock.EXPECT().Now().Return(referenceNow).Maybe()

	_, err := fixture.crontabEditApplication.CreateCronJob("every tuesday", "/bin/x", "", true)

	assert.ErrorIs(t, err, entity.ErrInvalidCronExpression)
}

func TestAdoptCronJobWalksTheWholeStack(t *testing.T) {
	fixture := newWritableFixture(t)
	fixture.givenCrontab(foreignRedirectCrontab)
	fixture.identifiers.EXPECT().NewIdentifier().Return("job-adopted")
	fixture.clock.EXPECT().Now().Return(referenceNow)

	savedContent := ""
	fixture.crontabRepository.EXPECT().Save(mock.Anything, "fingerprint-1").
		Run(func(document *entity.CrontabDocument, expectedFingerprint string) {
			savedContent = document.Render()
		}).Return(nil)

	foreignJobID := entity.ParseCrontabDocument(foreignRedirectCrontab).Jobs()[0].JobID()

	jobDto, err := fixture.crontabEditApplication.AdoptCronJob(foreignJobID)

	require.NoError(t, err)
	assert.Equal(t, "managed", jobDto.Origin)
	assert.Equal(t, "/data/logs/job-adopted.log", jobDto.LogFilePath)

	assert.Equal(t,
		"# cronwatch:id=job-adopted\n"+
			"# cronwatch:strippedRedirect= >> /var/log/y.log 2>&1\n"+
			"0 3 * * * /app/cronwatch run --job=job-adopted -- /bin/y\n",
		savedContent)
}

func TestSetCronJobEnabledRoundTripsThroughTheWholeStack(t *testing.T) {
	originalContent := "0    3 * * *   /bin/y >> /var/log/y.log 2>&1\n"

	disableFixture := newWritableFixture(t)
	disableFixture.givenCrontab(originalContent)
	disableFixture.clock.EXPECT().Now().Return(referenceNow)

	disabledContent := ""
	disableFixture.crontabRepository.EXPECT().Save(mock.Anything, "fingerprint-1").
		Run(func(document *entity.CrontabDocument, expectedFingerprint string) {
			disabledContent = document.Render()
		}).Return(nil)

	jobID := entity.ParseCrontabDocument(originalContent).Jobs()[0].JobID()

	_, err := disableFixture.crontabEditApplication.SetCronJobEnabled(jobID, false)
	require.NoError(t, err)
	require.Equal(t, "#0    3 * * *   /bin/y >> /var/log/y.log 2>&1\n", disabledContent)

	enableFixture := newWritableFixture(t)
	enableFixture.givenCrontab(disabledContent)
	enableFixture.clock.EXPECT().Now().Return(referenceNow)

	reEnabledContent := ""
	enableFixture.crontabRepository.EXPECT().Save(mock.Anything, "fingerprint-1").
		Run(func(document *entity.CrontabDocument, expectedFingerprint string) {
			reEnabledContent = document.Render()
		}).Return(nil)

	_, err = enableFixture.crontabEditApplication.SetCronJobEnabled(jobID, true)
	require.NoError(t, err)

	assert.Equal(t, originalContent, reEnabledContent,
		"a disable/enable round trip through the full stack restores the original bytes")
}

func TestDeleteCronJobWalksTheWholeStack(t *testing.T) {
	fixture := newWritableFixture(t)
	fixture.givenCrontab("# keep me\n" + managedJobCrontab + "0 9 * * * /bin/other\n")

	savedContent := ""
	fixture.crontabRepository.EXPECT().Save(mock.Anything, "fingerprint-1").
		Run(func(document *entity.CrontabDocument, expectedFingerprint string) {
			savedContent = document.Render()
		}).Return(nil)

	require.NoError(t, fixture.crontabEditApplication.DeleteCronJob("job-1"))

	assert.Equal(t, "# keep me\n0 9 * * * /bin/other\n", savedContent)
}

func TestCrontabWriteConflictSurfacesUnchanged(t *testing.T) {
	fixture := newWritableFixture(t)
	fixture.givenCrontab("")
	fixture.identifiers.EXPECT().NewIdentifier().Return("job-new")
	fixture.clock.EXPECT().Now().Return(referenceNow)
	fixture.crontabRepository.EXPECT().Save(mock.Anything, "fingerprint-1").
		Return(entity.ErrCrontabChangedExternally)

	_, err := fixture.crontabEditApplication.CreateCronJob("0 3 * * *", "/bin/x", "", true)

	assert.ErrorIs(t, err, entity.ErrCrontabChangedExternally,
		"the controller needs this sentinel intact to answer 409")
}
