package entity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

const twoManagedJobs = "# cronwatch:id=job-1\n" +
	"# cronwatch:description=Nightly backup\n" +
	"0 3 * * * /app/cronwatch run --job=job-1 -- /backup.sh\n" +
	"# cronwatch:id=job-2\n" +
	"0 4 * * * /app/cronwatch run --job=job-2 -- /sync.sh\n"

func statusOf(t *testing.T, crontabContent string, latestRuns map[string]*JobRun) *DesktopStatus {
	t.Helper()

	return NewDesktopStatus(ParseCrontabDocument(crontabContent).Jobs(), latestRuns, desktopNow)
}

// 應用剛啟動時看到的失敗，是它沒開著的期間發生的。那些不補通知 —— 一開起來就被
// 一串過期通知轟炸，只會讓人把通知關掉。
func TestFailureNoticeLedgerDoesNotAnnounceFailuresItFindsOnItsFirstLook(t *testing.T) {
	ledger := NewFailureNoticeLedger()

	notices := ledger.Reconcile(statusOf(t, twoManagedJobs, map[string]*JobRun{
		"job-1": failedRun("run-1", "job-1"),
		"job-2": failedRun("run-2", "job-2"),
	}))

	assert.Empty(t, notices)
}

// 啟動之後才出現的失敗才是「剛剛發生的事」，值得打擾使用者。
func TestFailureNoticeLedgerAnnouncesAFailureThatAppearsLater(t *testing.T) {
	ledger := NewFailureNoticeLedger()
	ledger.Reconcile(statusOf(t, twoManagedJobs, map[string]*JobRun{
		"job-1": succeededRun("run-1", "job-1"),
	}))

	notices := ledger.Reconcile(statusOf(t, twoManagedJobs, map[string]*JobRun{
		"job-1": succeededRun("run-1", "job-1"),
		"job-2": failedRun("run-2", "job-2"),
	}))

	require.Len(t, notices, 1)
	assert.Equal(t, "run-2", notices[0].RunID())
	assert.Equal(t, "job-2", notices[0].JobID())
	assert.Contains(t, notices[0].NotificationTitle(), "/sync.sh",
		"the notice must name the job, or it is useless without opening anything")
}

func TestFailureNoticeLedgerStaysQuietForOutcomesThatAreNotFailures(t *testing.T) {
	testCases := []struct {
		name           string
		crontabContent string
		laterRun       *JobRun
	}{
		{
			name:           "a run that succeeded",
			crontabContent: twoManagedJobs,
			laterRun:       succeededRun("run-2", "job-2"),
		},
		{
			name:           "a run that is still going",
			crontabContent: twoManagedJobs,
			laterRun:       runningRun("run-2", "job-2"),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ledger := NewFailureNoticeLedger()
			ledger.Reconcile(statusOf(t, testCase.crontabContent, map[string]*JobRun{}))

			notices := ledger.Reconcile(statusOf(t, testCase.crontabContent,
				map[string]*JobRun{"job-2": testCase.laterRun}))

			assert.Empty(t, notices)
		})
	}
}

// 未納管的 job 沒有可信的結果，因此也沒有可信的失敗。就算檔案裡有它的紀錄，
// 那也不代表 cron 跑的那一次。
func TestFailureNoticeLedgerStaysQuietForAJobItDoesNotManage(t *testing.T) {
	const foreignJob = "0 5 * * * /clean.sh\n"

	jobID := ParseCrontabDocument(foreignJob).Jobs()[0].JobID()

	ledger := NewFailureNoticeLedger()
	ledger.Reconcile(statusOf(t, foreignJob, map[string]*JobRun{}))

	notices := ledger.Reconcile(statusOf(t, foreignJob,
		map[string]*JobRun{jobID: failedRun("run-1", jobID)}))

	assert.Empty(t, notices)
}

// 逾時中止與「跑完了但失敗」是兩件不同的事，通知必須看得出差別。
func TestFailureNoticeLedgerTellsATimeoutApartFromAFailure(t *testing.T) {
	ledger := NewFailureNoticeLedger()
	ledger.Reconcile(statusOf(t, twoManagedJobs, map[string]*JobRun{}))

	notices := ledger.Reconcile(statusOf(t, twoManagedJobs,
		map[string]*JobRun{"job-1": timedOutRun("run-1", "job-1")}))

	require.Len(t, notices, 1)
	assert.Equal(t, FailureKindTimedOut, notices[0].Kind())

	failureNotices := NewFailureNoticeLedger()
	failureNotices.Reconcile(statusOf(t, twoManagedJobs, map[string]*JobRun{}))
	plainFailure := failureNotices.Reconcile(statusOf(t, twoManagedJobs,
		map[string]*JobRun{"job-1": failedRun("run-2", "job-1")}))

	require.Len(t, plainFailure, 1)
	assert.Equal(t, FailureKindFailed, plainFailure[0].Kind())
	assert.NotEqual(t, notices[0].NotificationTitle(), plainFailure[0].NotificationTitle(),
		"a timeout and a failure must not read as the same event")
}

// 同一次失敗只值得通知一次。每 30 秒重複轟炸同一件事，跟沒有通知一樣糟。
func TestFailureNoticeLedgerAnnouncesTheSameFailureOnlyOnce(t *testing.T) {
	ledger := NewFailureNoticeLedger()
	ledger.Reconcile(statusOf(t, twoManagedJobs, map[string]*JobRun{}))

	failure := map[string]*JobRun{"job-1": failedRun("run-1", "job-1")}

	require.Len(t, ledger.Reconcile(statusOf(t, twoManagedJobs, failure)), 1)
	assert.Empty(t, ledger.Reconcile(statusOf(t, twoManagedJobs, failure)))
	assert.Empty(t, ledger.Reconcile(statusOf(t, twoManagedJobs, failure)))
}

// 不同次的失敗是不同的事件，各自值得一則。
func TestFailureNoticeLedgerAnnouncesEachSeparateFailure(t *testing.T) {
	ledger := NewFailureNoticeLedger()
	ledger.Reconcile(statusOf(t, twoManagedJobs, map[string]*JobRun{}))

	firstNotices := ledger.Reconcile(statusOf(t, twoManagedJobs,
		map[string]*JobRun{"job-1": failedRun("run-1", "job-1")}))
	secondNotices := ledger.Reconcile(statusOf(t, twoManagedJobs,
		map[string]*JobRun{"job-1": failedRun("run-2", "job-1")}))

	require.Len(t, firstNotices, 1)
	require.Len(t, secondNotices, 1)
	assert.Equal(t, "run-1", firstNotices[0].RunID())
	assert.Equal(t, "run-2", secondNotices[0].RunID())
}

// 一筆還在跑的紀錄不能被當成「已經看過了」，否則它結束成失敗時就永遠不會通知。
func TestFailureNoticeLedgerAnnouncesARunThatWasStillGoingWhenFirstSeen(t *testing.T) {
	ledger := NewFailureNoticeLedger()
	ledger.Reconcile(statusOf(t, twoManagedJobs,
		map[string]*JobRun{"job-1": runningRun("run-1", "job-1")}))

	notices := ledger.Reconcile(statusOf(t, twoManagedJobs,
		map[string]*JobRun{"job-1": failedRun("run-1", "job-1")}))

	require.Len(t, notices, 1)
	assert.Equal(t, "run-1", notices[0].RunID())
}

// 讀不到 crontab 的那一輪什麼都不知道，因此什麼都不能通知，也不能把記憶洗掉 ——
// 洗掉的話，恢復之後那些真正新出現的失敗就會被當成舊的而錯過。
func TestFailureNoticeLedgerIgnoresARoundThatCouldNotReadAnything(t *testing.T) {
	ledger := NewFailureNoticeLedger()
	ledger.Reconcile(statusOf(t, twoManagedJobs, map[string]*JobRun{}))

	assert.Empty(t, ledger.Reconcile(NewUnavailableDesktopStatus("crontab: permission denied")))

	notices := ledger.Reconcile(statusOf(t, twoManagedJobs,
		map[string]*JobRun{"job-1": failedRun("run-1", "job-1")}))

	require.Len(t, notices, 1, "a failure that appeared while the source was unreadable is still new")
}

// 第一次對帳如果剛好讀不到，記憶不能被當成已經建立 —— 否則之後看到的第一批失敗
// 會被誤判為「啟動前就存在」。
func TestFailureNoticeLedgerIsNotPrimedByAnUnreadableFirstRound(t *testing.T) {
	ledger := NewFailureNoticeLedger()

	assert.Empty(t, ledger.Reconcile(NewUnavailableDesktopStatus("crontab: permission denied")))

	notices := ledger.Reconcile(statusOf(t, twoManagedJobs,
		map[string]*JobRun{"job-1": failedRun("run-1", "job-1")}))

	assert.Empty(t, notices, "the first readable round is still the first look, so it only records")
}

func TestFailureNoticeContent(t *testing.T) {
	failure := NewFailureNotice("run-1", "job-1", "Nightly backup", FailureKindFailed, 3, true)
	assert.Contains(t, failure.NotificationTitle(), "Nightly backup")
	assert.Contains(t, failure.NotificationBody(), "3")

	timedOut := NewFailureNotice("run-2", "job-2", "Photo sync", FailureKindTimedOut, 0, false)
	assert.Contains(t, timedOut.NotificationTitle(), "Photo sync")
	assert.NotEmpty(t, timedOut.NotificationBody())

	unknownExitCode := NewFailureNotice("run-3", "job-3", "Report", FailureKindFailed, 0, false)
	assert.NotContains(t, unknownExitCode.NotificationBody(), "0",
		"an unknown exit code must not be reported as 0, which means success")
}

func TestNewFailureKindNormalisesUnknownValues(t *testing.T) {
	assert.Equal(t, FailureKindTimedOut, NewFailureKind("timedOut"))
	assert.Equal(t, FailureKindFailed, NewFailureKind("failed"))
	assert.Equal(t, FailureKindFailed, NewFailureKind("exploded"))
}
