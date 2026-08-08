package entity_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

const desktopSummaryLimit = 20

// 摘要一次要說清楚三件事：這是哪個 job、下次什麼時候跑、上次的結果。三種結果
// 必須各自明確，尤其「無從得知」不能長得像成功。
func TestDesktopStatusLinesReportEachOutcome(t *testing.T) {
	crontabContent := managedJobLine("job-1", "0 3 * * *", "/backup.sh") +
		managedJobLine("job-2", "0 4 * * *", "/sync.sh") +
		"0 5 * * * /clean.sh >> /var/log/clean.log 2>&1\n"

	jobs := ParseCrontabDocument(crontabContent).Jobs()
	require.Len(t, jobs, 3)

	latestRuns := map[string]*JobRun{
		"job-1": succeededRun("run-1", "job-1"),
		"job-2": failedRun("run-2", "job-2"),
	}

	lines, omittedCount := NewDesktopStatus(jobs, latestRuns, desktopNow).Lines(desktopSummaryLimit)

	require.Len(t, lines, 3)
	assert.Zero(t, omittedCount)

	outcomeByJobID := map[string]string{}
	for _, line := range lines {
		outcomeByJobID[line.JobID] = line.Outcome
	}

	assert.Equal(t, string(LatestRunOutcomeSucceeded), outcomeByJobID["job-1"])
	assert.Equal(t, string(LatestRunOutcomeFailed), outcomeByJobID["job-2"])
	assert.Equal(t, string(LatestRunOutcomeUnknown), outcomeByJobID[jobs[2].JobID()],
		"a job the service does not wrap can only be reported as unknown")
}

// 一個未納管、輸出又沒被導到任何地方的 job，是最容易被誤報成「成功」的形態。
func TestDesktopStatusLinesReportUnknownForAJobWithNowhereToLook(t *testing.T) {
	jobs := ParseCrontabDocument("0 5 * * * /clean.sh\n").Jobs()
	require.Len(t, jobs, 1)

	lines, _ := NewDesktopStatus(jobs, nil, desktopNow).Lines(desktopSummaryLimit)

	require.Len(t, lines, 1)
	assert.Equal(t, string(LatestRunOutcomeUnknown), lines[0].Outcome)
	assert.NotEmpty(t, lines[0].Outcome, "an empty outcome would read as a blank cell, not as unknown")
}

func TestDesktopStatusLinesOfAnEmptyCrontab(t *testing.T) {
	lines, omittedCount := NewDesktopStatus(ParseCrontabDocument("").Jobs(), nil, desktopNow).Lines(desktopSummaryLimit)

	assert.Empty(t, lines)
	assert.Zero(t, omittedCount)
}

// 下次執行時間不猜。沒有可預測的下次執行、或條目已被停用，都必須留 nil，讓外殼
// 去說「不適用」，而不是畫出一個看起來很真的日期。
func TestDesktopStatusLinesLeaveNextRunUnsetWhenThereIsNone(t *testing.T) {
	testCases := []struct {
		name            string
		crontabContent  string
		expectedEnabled bool
	}{
		{
			name:            "a job that only runs at boot",
			crontabContent:  "@reboot /warm-cache.sh\n",
			expectedEnabled: true,
		},
		{
			name:            "a job that has been disabled",
			crontabContent:  "# 0 3 * * * /backup.sh\n",
			expectedEnabled: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			jobs := ParseCrontabDocument(testCase.crontabContent).Jobs()
			require.Len(t, jobs, 1)

			lines, _ := NewDesktopStatus(jobs, nil, desktopNow).Lines(desktopSummaryLimit)

			require.Len(t, lines, 1)
			assert.Nil(t, lines[0].NextRunAt)
			assert.Equal(t, testCase.expectedEnabled, lines[0].Enabled)
		})
	}
}

func TestDesktopStatusLinesCarryNameAndScheduleDescription(t *testing.T) {
	jobs := ParseCrontabDocument("0 3 * * * /usr/local/bin/backup.sh\n").Jobs()
	require.Len(t, jobs, 1)

	lines, _ := NewDesktopStatus(jobs, nil, desktopNow).Lines(desktopSummaryLimit)

	require.Len(t, lines, 1)
	assert.Equal(t, jobs[0].DisplayName(), lines[0].DisplayName)
	assert.Equal(t, jobs[0].Schedule().Describe(), lines[0].ScheduleDescription)
	require.NotNil(t, lines[0].NextRunAt)
	assert.Equal(t, time.Date(2026, 8, 8, 3, 0, 0, 0, taipeiLocation), *lines[0].NextRunAt)
}

// 一份很長的 crontab 不該把選單列拉成一片牆。截斷是可以的，但被截掉幾筆必須說
// 出來 —— 安靜地只顯示前 20 筆，會被讀成「總共就這些」。
func TestDesktopStatusLinesAreTruncatedAndSayHowMany(t *testing.T) {
	crontabContent := ""
	for index := 0; index < 25; index++ {
		crontabContent += fmt.Sprintf("%d 3 * * * /job-%02d.sh\n", index, index)
	}

	jobs := ParseCrontabDocument(crontabContent).Jobs()
	require.Len(t, jobs, 25)

	lines, omittedCount := NewDesktopStatus(jobs, nil, desktopNow).Lines(desktopSummaryLimit)

	assert.Len(t, lines, desktopSummaryLimit)
	assert.Equal(t, 5, omittedCount)
}

func TestDesktopStatusLinesAreNotTruncatedWithoutALimit(t *testing.T) {
	crontabContent := ""
	for index := 0; index < 25; index++ {
		crontabContent += fmt.Sprintf("%d 3 * * * /job-%02d.sh\n", index, index)
	}

	lines, omittedCount := NewDesktopStatus(ParseCrontabDocument(crontabContent).Jobs(), nil, desktopNow).Lines(0)

	assert.Len(t, lines, 25)
	assert.Zero(t, omittedCount)
}

// 排序是為了讓最該被看到的東西不會被截斷掉：出事的排最前，接著是快要跑的，
// 不會再跑的排最後。
func TestDesktopStatusLinesAreOrderedByUrgency(t *testing.T) {
	crontabContent := "" +
		managedJobLine("soon", "30 1 * * *", "/soon.sh") +
		"@reboot /at-boot.sh\n" +
		managedJobLine("later", "0 5 * * *", "/later.sh") +
		managedJobLine("broken", "0 23 * * *", "/broken.sh")

	jobs := ParseCrontabDocument(crontabContent).Jobs()
	require.Len(t, jobs, 4)

	latestRuns := map[string]*JobRun{"broken": failedRun("run-1", "broken")}

	lines, _ := NewDesktopStatus(jobs, latestRuns, desktopNow).Lines(desktopSummaryLimit)

	require.Len(t, lines, 4)
	assert.Equal(t, "broken", lines[0].JobID, "what needs attention comes first")
	assert.True(t, lines[0].NeedsAttention)
	assert.Equal(t, "soon", lines[1].JobID, "then whatever runs next")
	assert.Equal(t, "later", lines[2].JobID)
	assert.Equal(t, jobs[1].JobID(), lines[3].JobID, "a job with no next run comes last")
	assert.False(t, lines[1].NeedsAttention)
}
