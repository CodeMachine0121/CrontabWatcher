package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/domain/interface/mocks"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

const triggerTimeout = 5 * time.Minute

type jobExecutionServiceFixture struct {
	service           *service.JobExecutionService
	crontabRepository *mocks.MockICrontabDocumentRepository
	jobRunRepository  *mocks.MockIJobRunRepository
	jobLogRepository  *mocks.MockIJobLogRepository
	commandProxy      *mocks.MockICommandExecutionProxy
	identifiers       *mocks.MockIIdentifierGenerator
	clock             *mocks.MockIClock
}

func newJobExecutionServiceFixture(t *testing.T) jobExecutionServiceFixture {
	t.Helper()

	crontabRepository := mocks.NewMockICrontabDocumentRepository(t)
	jobRunRepository := mocks.NewMockIJobRunRepository(t)
	jobLogRepository := mocks.NewMockIJobLogRepository(t)
	commandProxy := mocks.NewMockICommandExecutionProxy(t)
	identifiers := mocks.NewMockIIdentifierGenerator(t)
	clock := mocks.NewMockIClock(t)

	return jobExecutionServiceFixture{
		service: service.NewJobExecutionService(
			crontabRepository, jobRunRepository, jobLogRepository,
			commandProxy, identifiers, clock, managedLogDirectory),
		crontabRepository: crontabRepository,
		jobRunRepository:  jobRunRepository,
		jobLogRepository:  jobLogRepository,
		commandProxy:      commandProxy,
		identifiers:       identifiers,
		clock:             clock,
	}
}

func (fixture jobExecutionServiceFixture) givenCrontab(content string) {
	fixture.crontabRepository.EXPECT().Load().
		Return(entity.ParseCrontabDocument(content), "fingerprint", nil)
}

// givenAReadyRunRecorder 設定「沒有執行中的紀錄、識別碼固定、時鐘先回開始時刻
// 再回結束時刻」這組最常見的前置條件。
func (fixture jobExecutionServiceFixture) givenAReadyRunRecorder(jobID string, elapsed time.Duration) {
	fixture.jobRunRepository.EXPECT().HasRunningRun(jobID).Return(false, nil)
	fixture.identifiers.EXPECT().NewIdentifier().Return("run-fixed")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(elapsed)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)
	fixture.jobLogRepository.EXPECT().Append(mock.Anything, mock.Anything).Return(nil).Maybe()
}

func TestTriggerJobRunRecordsASuccessfulRun(t *testing.T) {
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.givenAReadyRunRecorder("job-1", 1204*time.Millisecond)

	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/x", triggerTimeout).
		Return(vo.NewCommandExecutionResult("all good\n", 0, false), nil)

	runDto, err := fixture.service.TriggerJobRun(context.Background(), "job-1", triggerTimeout)

	require.NoError(t, err)
	assert.Equal(t, "run-fixed", runDto.RunID)
	assert.Equal(t, "job-1", runDto.JobID)
	assert.Equal(t, "manual", runDto.TriggerSource)
	assert.Equal(t, "succeeded", runDto.RunStatus)
	assert.Equal(t, "all good\n", runDto.OutputExcerpt)

	require.NotNil(t, runDto.ExitCode)
	assert.Equal(t, 0, *runDto.ExitCode)
	require.NotNil(t, runDto.DurationMilliseconds)
	assert.Equal(t, int64(1204), *runDto.DurationMilliseconds)
}

func TestTriggerJobRunRecordsAFailedRun(t *testing.T) {
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.givenAReadyRunRecorder("job-1", time.Second)

	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/x", triggerTimeout).
		Return(vo.NewCommandExecutionResult("boom\n", 3, false), nil)

	runDto, err := fixture.service.TriggerJobRun(context.Background(), "job-1", triggerTimeout)

	require.NoError(t, err, "a failing job is not a failure of the trigger")
	assert.Equal(t, "failed", runDto.RunStatus)
	require.NotNil(t, runDto.ExitCode)
	assert.Equal(t, 3, *runDto.ExitCode)
}

func TestTriggerJobRunRecordsATimedOutRun(t *testing.T) {
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.givenAReadyRunRecorder("job-1", triggerTimeout)

	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/x", triggerTimeout).
		Return(vo.NewCommandExecutionResult("partial\n", -1, true), nil)

	runDto, err := fixture.service.TriggerJobRun(context.Background(), "job-1", triggerTimeout)

	require.NoError(t, err)
	assert.Equal(t, "timedOut", runDto.RunStatus)
}

func TestTriggerJobRunExecutesTheInnerCommandNotTheWrapper(t *testing.T) {
	// 執行 wrapper 全文會讓 wrapper 再呼叫自己，無止盡遞迴下去。
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.givenAReadyRunRecorder("job-1", time.Second)

	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/x", triggerTimeout).
		Return(vo.NewCommandExecutionResult("", 0, false), nil)

	_, err := fixture.service.TriggerJobRun(context.Background(), "job-1", triggerTimeout)

	require.NoError(t, err)
	fixture.commandProxy.AssertNotCalled(t, "Execute", mock.Anything,
		"/app/cronwatch run --job=job-1 -- /bin/x", mock.Anything)
}

func TestTriggerJobRunStripsTheRedirectBeforeExecuting(t *testing.T) {
	// 留著 redirect 會讓輸出在到達我們手上之前就被導走，紀錄就成了空的。
	crontabContent := "0 3 * * * /bin/x >> /var/log/x.log 2>&1\n"
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(crontabContent)

	foreignJobID := entity.ParseCrontabDocument(crontabContent).Jobs()[0].JobID()
	fixture.givenAReadyRunRecorder(foreignJobID, time.Second)

	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/x", triggerTimeout).
		Return(vo.NewCommandExecutionResult("captured\n", 0, false), nil)

	runDto, err := fixture.service.TriggerJobRun(context.Background(), foreignJobID, triggerTimeout)

	require.NoError(t, err)
	assert.Equal(t, "captured\n", runDto.OutputExcerpt)
}

func TestTriggerJobRunWritesOutputToTheJobsOwnLogFile(t *testing.T) {
	// foreign job 的輸出寫回它自己設定的 redirect 目標，而不是我們的目錄 ——
	// 使用者去看那個檔案時，手動觸發的那次也該在裡面。
	crontabContent := "0 3 * * * /bin/x >> /var/log/x.log 2>&1\n"
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(crontabContent)

	foreignJobID := entity.ParseCrontabDocument(crontabContent).Jobs()[0].JobID()

	fixture.jobRunRepository.EXPECT().HasRunningRun(foreignJobID).Return(false, nil)
	fixture.identifiers.EXPECT().NewIdentifier().Return("run-fixed")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)

	writtenLogPaths := make([]string, 0)
	fixture.jobLogRepository.EXPECT().Append(mock.Anything, mock.Anything).
		Run(func(filePath string, content string) {
			writtenLogPaths = append(writtenLogPaths, filePath)
		}).Return(nil)

	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/x", triggerTimeout).
		Return(vo.NewCommandExecutionResult("captured\n", 0, false), nil)

	_, err := fixture.service.TriggerJobRun(context.Background(), foreignJobID, triggerTimeout)

	require.NoError(t, err)
	require.NotEmpty(t, writtenLogPaths)
	for _, logPath := range writtenLogPaths {
		assert.Equal(t, "/var/log/x.log", logPath)
	}
}

func TestTriggerJobRunFallsBackToTheManagedDirectoryWhenThereIsNoLogDestination(t *testing.T) {
	crontabContent := "0 3 * * * /bin/x\n"
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(crontabContent)

	foreignJobID := entity.ParseCrontabDocument(crontabContent).Jobs()[0].JobID()

	fixture.jobRunRepository.EXPECT().HasRunningRun(foreignJobID).Return(false, nil)
	fixture.identifiers.EXPECT().NewIdentifier().Return("run-fixed")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)

	fixture.jobLogRepository.EXPECT().
		Append("/data/logs/"+foreignJobID+".log", mock.Anything).Return(nil)

	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/x", triggerTimeout).
		Return(vo.NewCommandExecutionResult("captured\n", 0, false), nil)

	_, err := fixture.service.TriggerJobRun(context.Background(), foreignJobID, triggerTimeout)

	require.NoError(t, err)
}

func TestTriggerJobRunRefusesWhenTheJobIsAlreadyRunning(t *testing.T) {
	// cronjob 多半不是 idempotent。重複觸發的代價高過讓使用者等。
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.jobRunRepository.EXPECT().HasRunningRun("job-1").Return(true, nil)

	_, err := fixture.service.TriggerJobRun(context.Background(), "job-1", triggerTimeout)

	assert.ErrorIs(t, err, entity.ErrJobRunAlreadyRunning)
	fixture.commandProxy.AssertNotCalled(t, "Execute", mock.Anything, mock.Anything, mock.Anything)
}

func TestTriggerJobRunRejectsAnUnknownJob(t *testing.T) {
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)

	_, err := fixture.service.TriggerJobRun(context.Background(), "does-not-exist", triggerTimeout)

	assert.ErrorIs(t, err, entity.ErrCronJobNotFound)
	fixture.commandProxy.AssertNotCalled(t, "Execute", mock.Anything, mock.Anything, mock.Anything)
}

func TestTriggerJobRunFailsLoudlyWhenTheRunCannotBeRecorded(t *testing.T) {
	// 手動觸發時，記不下來就先不要跑 —— 使用者按了按鈕卻得不到任何結果回饋，
	// 比根本沒跑更糟。
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.jobRunRepository.EXPECT().HasRunningRun("job-1").Return(false, nil)
	fixture.identifiers.EXPECT().NewIdentifier().Return("run-fixed")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()

	appendFailure := errors.New("read-only file system")
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(appendFailure)

	_, err := fixture.service.TriggerJobRun(context.Background(), "job-1", triggerTimeout)

	assert.ErrorIs(t, err, appendFailure)
	fixture.commandProxy.AssertNotCalled(t, "Execute", mock.Anything, mock.Anything, mock.Anything)
}

func TestTriggerJobRunSurvivesALogWriteFailure(t *testing.T) {
	// 監控工具寫不進 log，不該讓使用者的 job 跑不起來。但問題必須出現在使用者
	// 會看的地方，不能默默吞掉。
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.jobRunRepository.EXPECT().HasRunningRun("job-1").Return(false, nil)
	fixture.identifiers.EXPECT().NewIdentifier().Return("run-fixed")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)

	fixture.jobLogRepository.EXPECT().Append(mock.Anything, mock.Anything).
		Return(errors.New("no space left on device"))

	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/x", triggerTimeout).
		Return(vo.NewCommandExecutionResult("job output\n", 0, false), nil)

	runDto, err := fixture.service.TriggerJobRun(context.Background(), "job-1", triggerTimeout)

	require.NoError(t, err, "the job ran; our own logging problem must not fail it")
	assert.Equal(t, "succeeded", runDto.RunStatus)
	assert.Contains(t, runDto.OutputExcerpt, "job output")
	assert.Contains(t, runDto.OutputExcerpt, "no space left on device",
		"the logging failure is surfaced where the user will actually look")
}

func TestTriggerJobRunReportsAnExecutionFailure(t *testing.T) {
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.jobRunRepository.EXPECT().HasRunningRun("job-1").Return(false, nil)
	fixture.identifiers.EXPECT().NewIdentifier().Return("run-fixed")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)
	fixture.jobLogRepository.EXPECT().Append(mock.Anything, mock.Anything).Return(nil).Maybe()

	executionFailure := errors.New("cannot start shell")
	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/x", triggerTimeout).
		Return(vo.CommandExecutionResult{}, executionFailure)

	runDto, err := fixture.service.TriggerJobRun(context.Background(), "job-1", triggerTimeout)

	require.Error(t, err, "we could not carry the execution through, so this is our failure")
	assert.Equal(t, "unknown", runDto.RunStatus,
		"the run must not be left as running, and we cannot claim it failed on its own terms")
}

func TestTriggerJobRunTruncatesAVeryLargeOutputInTheRecord(t *testing.T) {
	fixture := newJobExecutionServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.givenAReadyRunRecorder("job-1", time.Second)

	largeOutput := strings.Repeat("x", 9*1024) + "TAIL MARKER"
	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/x", triggerTimeout).
		Return(vo.NewCommandExecutionResult(largeOutput, 0, false), nil)

	runDto, err := fixture.service.TriggerJobRun(context.Background(), "job-1", triggerTimeout)

	require.NoError(t, err)
	assert.True(t, runDto.OutputTruncated)
	assert.LessOrEqual(t, len(runDto.OutputExcerpt), entity.JobRunOutputExcerptMaxBytes)
	assert.True(t, strings.HasSuffix(runDto.OutputExcerpt, "TAIL MARKER"))
}

func TestRecordWrapperRunUsesTheScheduleTriggerAndNoTimeout(t *testing.T) {
	// 排程觸發的執行該跑多久就跑多久，也不做並發檢查 —— 是 cron 決定要跑的，
	// 重疊執行是使用者自己的排程問題。
	fixture := newJobExecutionServiceFixture(t)

	fixture.identifiers.EXPECT().NewIdentifier().Return("run-wrapper")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(2 * time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)
	fixture.jobLogRepository.EXPECT().Append("/data/logs/job-1.log", mock.Anything).Return(nil)

	fixture.commandProxy.EXPECT().
		Execute(mock.Anything, "/bin/backup.sh", time.Duration(0)).
		Return(vo.NewCommandExecutionResult("done\n", 0, false), nil)

	runDto, err := fixture.service.RecordWrapperRun(context.Background(), "job-1", "/bin/backup.sh")

	require.NoError(t, err)
	assert.Equal(t, "schedule", runDto.TriggerSource)
	assert.Equal(t, "succeeded", runDto.RunStatus)
	require.NotNil(t, runDto.ExitCode)
	assert.Equal(t, 0, *runDto.ExitCode)
}

func TestRecordWrapperRunDoesNotReadTheCrontab(t *testing.T) {
	// wrapper 從 argv 拿到指令，不需要讀 crontab —— 少一個可能讓 job 跑不起來的
	// 失敗點。mock 未設定 Load，一旦被呼叫測試就會失敗。
	fixture := newJobExecutionServiceFixture(t)

	fixture.identifiers.EXPECT().NewIdentifier().Return("run-wrapper")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)
	fixture.jobLogRepository.EXPECT().Append(mock.Anything, mock.Anything).Return(nil)
	fixture.commandProxy.EXPECT().Execute(mock.Anything, "/bin/x", time.Duration(0)).
		Return(vo.NewCommandExecutionResult("", 0, false), nil)

	_, err := fixture.service.RecordWrapperRun(context.Background(), "job-1", "/bin/x")

	require.NoError(t, err)
}

func TestRecordWrapperRunStillExecutesWhenTheRecordCannotBeWritten(t *testing.T) {
	// 這是 wrapper 與手動觸發的關鍵差異：cron 已經決定要跑這個 job，我們自己的
	// 紀錄檔壞掉不該把使用者的排程也一起弄壞。
	fixture := newJobExecutionServiceFixture(t)

	fixture.identifiers.EXPECT().NewIdentifier().Return("run-wrapper")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(errors.New("read-only file system"))
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(errors.New("read-only file system"))
	fixture.jobLogRepository.EXPECT().Append(mock.Anything, mock.Anything).Return(nil).Maybe()

	fixture.commandProxy.EXPECT().Execute(mock.Anything, "/bin/x", time.Duration(0)).
		Return(vo.NewCommandExecutionResult("ran anyway\n", 0, false), nil)

	runDto, err := fixture.service.RecordWrapperRun(context.Background(), "job-1", "/bin/x")

	require.NoError(t, err, "the job ran and exited cleanly; our bookkeeping failure is not its failure")
	assert.Equal(t, "succeeded", runDto.RunStatus)
	require.NotNil(t, runDto.ExitCode)
	assert.Equal(t, 0, *runDto.ExitCode)
}

func TestRecordWrapperRunWritesRunDelimitersIntoTheLog(t *testing.T) {
	fixture := newJobExecutionServiceFixture(t)

	fixture.identifiers.EXPECT().NewIdentifier().Return("run-wrapper")
	fixture.clock.EXPECT().Now().Return(referenceNow).Once()
	fixture.clock.EXPECT().Now().Return(referenceNow.Add(time.Second)).Once()
	fixture.jobRunRepository.EXPECT().Append(mock.Anything).Return(nil)
	fixture.jobRunRepository.EXPECT().Update(mock.Anything).Return(nil)

	appendedContent := make([]string, 0)
	fixture.jobLogRepository.EXPECT().Append(mock.Anything, mock.Anything).
		Run(func(filePath string, content string) {
			appendedContent = append(appendedContent, content)
		}).Return(nil)

	fixture.commandProxy.EXPECT().Execute(mock.Anything, "/bin/x", time.Duration(0)).
		Return(vo.NewCommandExecutionResult("the output\n", 0, false), nil)

	_, err := fixture.service.RecordWrapperRun(context.Background(), "job-1", "/bin/x")
	require.NoError(t, err)

	combinedContent := strings.Join(appendedContent, "")
	assert.Contains(t, combinedContent, "run-wrapper", "the delimiter carries the run identifier")
	assert.Contains(t, combinedContent, "the output\n")
	assert.Contains(t, combinedContent, "exit=0")
}
