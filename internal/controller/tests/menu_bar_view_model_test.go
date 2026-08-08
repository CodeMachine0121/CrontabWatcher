package controller_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/controller"
	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

var (
	taipeiLocation = mustLoadLocation("Asia/Taipei")
	nextRunAt      = time.Date(2026, 8, 8, 3, 0, 0, 0, taipeiLocation)
)

func mustLoadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}

	return location
}

func statusWithLines(indicator entity.StatusIndicator, lines ...dto.JobStatusLineDto) dto.DesktopStatusDto {
	return dto.DesktopStatusDto{Indicator: string(indicator), Lines: lines}
}

// 圖示是整個功能唯一「不點開就看得到」的資訊，三種狀態必須用形狀分得開，不能
// 只靠顏色 —— 深色模式與色覺差異都會讓顏色失效。
func TestMenuBarViewModelDistinguishesTheThreeIndicators(t *testing.T) {
	normal := controller.NewMenuBarViewModel(statusWithLines(entity.StatusIndicatorNormal))
	attention := controller.NewMenuBarViewModel(statusWithLines(entity.StatusIndicatorAttention))
	unavailable := controller.NewMenuBarViewModel(
		dto.DesktopStatusDto{Indicator: string(entity.StatusIndicatorUnavailable), UnavailableReason: "permission denied"})

	titles := []string{normal.IndicatorTitle, attention.IndicatorTitle, unavailable.IndicatorTitle}

	assert.Len(t, uniqueValues(titles), 3, "the three states must not look alike")
	assert.Contains(t, unavailable.Tooltip, "permission denied")
	assert.NotContains(t, normal.Tooltip, "沒有跑成功",
		"a normal state must not describe failures that are not there")
}

func TestMenuBarViewModelLineShowsOutcomeNameScheduleAndNextRun(t *testing.T) {
	viewModel := controller.NewMenuBarViewModel(statusWithLines(entity.StatusIndicatorAttention,
		dto.JobStatusLineDto{
			JobID:               "job-1",
			DisplayName:         "Nightly backup",
			ScheduleDescription: "每天 03:00",
			NextRunAt:           &nextRunAt,
			Enabled:             true,
			Outcome:             string(entity.LatestRunOutcomeFailed),
			NeedsAttention:      true,
		}))

	require.Len(t, viewModel.LineTitles, 1)
	require.Equal(t, []string{"job-1"}, viewModel.LineJobIDs)

	title := viewModel.LineTitles[0]
	assert.Contains(t, title, "Nightly backup")
	assert.Contains(t, title, "每天 03:00")
	assert.Contains(t, title, "08/08 03:00")
	assert.Contains(t, viewModel.Tooltip, "1")
}

// 四種結果必須各自看得出來。「無從得知」長得像成功是這個功能最不能犯的錯。
func TestMenuBarViewModelGivesEachOutcomeItsOwnSymbol(t *testing.T) {
	outcomes := []entity.LatestRunOutcome{
		entity.LatestRunOutcomeSucceeded,
		entity.LatestRunOutcomeFailed,
		entity.LatestRunOutcomeRunning,
		entity.LatestRunOutcomeUnknown,
	}

	titles := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		viewModel := controller.NewMenuBarViewModel(statusWithLines(entity.StatusIndicatorNormal,
			dto.JobStatusLineDto{DisplayName: "x", Enabled: true, NextRunAt: &nextRunAt, Outcome: string(outcome)}))
		titles = append(titles, viewModel.LineTitles[0])
	}

	assert.Len(t, uniqueValues(titles), len(outcomes))
}

func TestMenuBarViewModelSaysWhenThereIsNoNextRun(t *testing.T) {
	testCases := []struct {
		name         string
		line         dto.JobStatusLineDto
		expectedText string
	}{
		{
			name:         "a disabled job",
			line:         dto.JobStatusLineDto{DisplayName: "x", Enabled: false, Outcome: "unknown"},
			expectedText: "已停用",
		},
		{
			name:         "a job with no predictable next run",
			line:         dto.JobStatusLineDto{DisplayName: "x", Enabled: true, Outcome: "unknown"},
			expectedText: "不適用",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			viewModel := controller.NewMenuBarViewModel(
				statusWithLines(entity.StatusIndicatorNormal, testCase.line))

			require.Len(t, viewModel.LineTitles, 1)
			assert.Contains(t, viewModel.LineTitles[0], testCase.expectedText)
		})
	}
}

// 被截掉的筆數要說出來。安靜地只畫前 20 筆會被讀成「總共就這些」。
func TestMenuBarViewModelSaysHowManyLinesWereLeftOut(t *testing.T) {
	status := statusWithLines(entity.StatusIndicatorNormal,
		dto.JobStatusLineDto{DisplayName: "x", Enabled: true, Outcome: "unknown"})
	status.OmittedLineCount = 5

	viewModel := controller.NewMenuBarViewModel(status)

	assert.Contains(t, viewModel.OverflowTitle, "5")
	assert.Empty(t, viewModel.EmptyMessage)
}

func TestMenuBarViewModelHasNoOverflowNoticeWhenNothingWasLeftOut(t *testing.T) {
	viewModel := controller.NewMenuBarViewModel(statusWithLines(entity.StatusIndicatorNormal,
		dto.JobStatusLineDto{DisplayName: "x", Enabled: true, Outcome: "unknown"}))

	assert.Empty(t, viewModel.OverflowTitle)
}

// 「真的沒有排程」與「讀不到」是兩件不同的事。把後者說成前者，等於在使用者的
// crontab 明明有東西的時候告訴他那裡是空的。
func TestMenuBarViewModelTellsAnEmptyCrontabApartFromAnUnreadableOne(t *testing.T) {
	empty := controller.NewMenuBarViewModel(statusWithLines(entity.StatusIndicatorNormal))
	unreadable := controller.NewMenuBarViewModel(dto.DesktopStatusDto{
		Indicator:         string(entity.StatusIndicatorUnavailable),
		UnavailableReason: "crontab: permission denied",
	})

	assert.Equal(t, "目前沒有排程", empty.EmptyMessage)
	assert.Contains(t, unreadable.EmptyMessage, "permission denied")
	assert.NotEqual(t, empty.EmptyMessage, unreadable.EmptyMessage)
}

func uniqueValues(values []string) map[string]bool {
	unique := map[string]bool{}
	for _, value := range values {
		unique[value] = true
	}

	return unique
}
