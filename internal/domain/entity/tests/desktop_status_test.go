package entity_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

var desktopNow = time.Date(2026, 8, 8, 1, 0, 0, 0, taipeiLocation)

// managedJobLine 產生一行納管條目。納管的身分來自 marker 註解，所以兩行一組。
func managedJobLine(jobID string, schedule string, command string) string {
	return "# cronwatch:id=" + jobID + "\n" +
		schedule + " /app/cronwatch run --job=" + jobID + " -- " + command + "\n"
}

func succeededRun(runID string, jobID string) *JobRun {
	run := NewJobRun(runID, jobID, TriggerSourceSchedule, desktopNow)
	run.Finish(desktopNow.Add(time.Second), 0, false, "")

	return run
}

func failedRun(runID string, jobID string) *JobRun {
	run := NewJobRun(runID, jobID, TriggerSourceSchedule, desktopNow)
	run.Finish(desktopNow.Add(time.Second), 3, false, "boom")

	return run
}

func timedOutRun(runID string, jobID string) *JobRun {
	run := NewJobRun(runID, jobID, TriggerSourceSchedule, desktopNow)
	run.Finish(desktopNow.Add(time.Second), -1, true, "")

	return run
}

func runningRun(runID string, jobID string) *JobRun {
	return NewJobRun(runID, jobID, TriggerSourceSchedule, desktopNow)
}

// 圖示只回答一個問題：現在有沒有事要理。它刻意不累積歷史，也刻意不把「無從得知」
// 當成壞消息。
func TestDesktopStatusIndicator(t *testing.T) {
	testCases := []struct {
		name              string
		crontabContent    string
		latestRuns        map[string]*JobRun
		expectedIndicator StatusIndicator
	}{
		{
			name: "every managed job succeeded last time",
			crontabContent: managedJobLine("job-1", "0 3 * * *", "/backup.sh") +
				managedJobLine("job-2", "0 4 * * *", "/sync.sh") +
				managedJobLine("job-3", "0 5 * * *", "/clean.sh"),
			latestRuns: map[string]*JobRun{
				"job-1": succeededRun("run-1", "job-1"),
				"job-2": succeededRun("run-2", "job-2"),
				"job-3": succeededRun("run-3", "job-3"),
			},
			expectedIndicator: StatusIndicatorNormal,
		},
		{
			name: "one managed job failed last time",
			crontabContent: managedJobLine("job-1", "0 3 * * *", "/backup.sh") +
				managedJobLine("job-2", "0 4 * * *", "/sync.sh"),
			latestRuns: map[string]*JobRun{
				"job-1": succeededRun("run-1", "job-1"),
				"job-2": failedRun("run-2", "job-2"),
			},
			expectedIndicator: StatusIndicatorAttention,
		},
		{
			name:              "one managed job was killed for running too long",
			crontabContent:    managedJobLine("job-1", "0 3 * * *", "/backup.sh"),
			latestRuns:        map[string]*JobRun{"job-1": timedOutRun("run-1", "job-1")},
			expectedIndicator: StatusIndicatorAttention,
		},
		{
			name:              "only jobs the service has not wrapped",
			crontabContent:    "0 3 * * * /backup.sh\n0 4 * * * /sync.sh\n",
			latestRuns:        map[string]*JobRun{},
			expectedIndicator: StatusIndicatorNormal,
		},
		{
			name:              "a managed job that has never run",
			crontabContent:    managedJobLine("job-1", "0 3 * * *", "/backup.sh"),
			latestRuns:        map[string]*JobRun{},
			expectedIndicator: StatusIndicatorNormal,
		},
		{
			name:              "a managed job that is still running",
			crontabContent:    managedJobLine("job-1", "0 3 * * *", "/backup.sh"),
			latestRuns:        map[string]*JobRun{"job-1": runningRun("run-1", "job-1")},
			expectedIndicator: StatusIndicatorNormal,
		},
		{
			name:              "a job that failed before but succeeded most recently",
			crontabContent:    managedJobLine("job-1", "0 3 * * *", "/backup.sh"),
			latestRuns:        map[string]*JobRun{"job-1": succeededRun("run-2", "job-1")},
			expectedIndicator: StatusIndicatorNormal,
		},
		{
			name:              "an empty crontab",
			crontabContent:    "",
			latestRuns:        map[string]*JobRun{},
			expectedIndicator: StatusIndicatorNormal,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			jobs := ParseCrontabDocument(testCase.crontabContent).Jobs()

			status := NewDesktopStatus(jobs, testCase.latestRuns, desktopNow)

			assert.Equal(t, testCase.expectedIndicator, status.Indicator())
			assert.Empty(t, status.UnavailableReason())
		})
	}
}

// 讀不到 crontab 與「一切正常」是完全不同的事實，混為一談會讓使用者以為沒事。
func TestDesktopStatusUnavailable(t *testing.T) {
	status := NewUnavailableDesktopStatus("crontab: permission denied")

	assert.Equal(t, StatusIndicatorUnavailable, status.Indicator())
	assert.Equal(t, "crontab: permission denied", status.UnavailableReason())

	lines, omittedCount := status.Lines(20)
	assert.Empty(t, lines, "nothing may be listed when the source could not be read")
	assert.Zero(t, omittedCount)
}

// 一個未納管的 job 即使剛好有執行紀錄（例如使用者手動觸發過），它按排程跑的那些
// 執行仍然無從得知結果。既有的執行歷史用例已經是這個規則，圖示不該有第二套。
func TestDesktopStatusIgnoresRunsOfJobsItDoesNotManage(t *testing.T) {
	jobs := ParseCrontabDocument("0 3 * * * /backup.sh\n").Jobs()
	require.Len(t, jobs, 1)

	latestRuns := map[string]*JobRun{jobs[0].JobID(): failedRun("run-1", jobs[0].JobID())}

	status := NewDesktopStatus(jobs, latestRuns, desktopNow)

	assert.Equal(t, StatusIndicatorNormal, status.Indicator())
}

func TestNewStatusIndicatorNormalisesUnknownValues(t *testing.T) {
	assert.Equal(t, StatusIndicatorAttention, NewStatusIndicator("attention"))
	assert.Equal(t, StatusIndicatorUnavailable, NewStatusIndicator("unavailable"))
	assert.Equal(t, StatusIndicatorNormal, NewStatusIndicator("normal"))
	assert.Equal(t, StatusIndicatorNormal, NewStatusIndicator("panic"),
		"an unrecognised indicator must not invent a problem that is not there")
}
