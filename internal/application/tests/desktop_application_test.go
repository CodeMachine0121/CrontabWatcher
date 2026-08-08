package application_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/application"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/domain/interface/mocks"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
)

const desktopSummaryLineLimit = 20

const desktopCrontab = "# cronwatch:id=job-1\n" +
	"# cronwatch:description=Nightly backup\n" +
	"0 3 * * * /app/cronwatch run --job=job-1 -- /backup.sh\n"

type desktopFixture struct {
	desktopApplication *application.DesktopApplication

	crontabRepository *mocks.MockICrontabDocumentRepository
	jobRunRepository  *mocks.MockIJobRunRepository
	notifications     *mocks.MockINotificationProxy
}

func newDesktopFixture(t *testing.T) desktopFixture {
	t.Helper()

	crontabRepository := mocks.NewMockICrontabDocumentRepository(t)
	jobRunRepository := mocks.NewMockIJobRunRepository(t)
	notifications := mocks.NewMockINotificationProxy(t)
	clock := mocks.NewMockIClock(t)
	clock.EXPECT().Now().Return(referenceNow).Maybe()

	desktopStatusService := service.NewDesktopStatusService(
		crontabRepository, jobRunRepository, desktopSummaryLineLimit)

	return desktopFixture{
		desktopApplication: application.NewDesktopApplication(desktopStatusService, clock, notifications),
		crontabRepository:  crontabRepository,
		jobRunRepository:   jobRunRepository,
		notifications:      notifications,
	}
}

func (fixture desktopFixture) givenCrontabAlwaysReadable() {
	fixture.crontabRepository.EXPECT().Load().
		Return(entity.ParseCrontabDocument(desktopCrontab), "fingerprint", nil)
}

func desktopRun(runID string, exitCode int) map[string]*entity.JobRun {
	run := entity.NewJobRun(runID, "job-1", entity.TriggerSourceSchedule, referenceNow)
	run.Finish(referenceNow, exitCode, false, "")

	return map[string]*entity.JobRun{"job-1": run}
}

// 選單列在第一次刷新完成前也得畫點什麼，而那一刻我們確實還不知道有沒有事要理。
func TestDesktopApplicationStartsWithAnEmptySnapshot(t *testing.T) {
	fixture := newDesktopFixture(t)

	snapshot := fixture.desktopApplication.Snapshot()

	assert.Equal(t, string(entity.StatusIndicatorNormal), snapshot.Indicator)
	assert.Empty(t, snapshot.Lines)
}

func TestDesktopApplicationRefreshStoresTheSnapshot(t *testing.T) {
	fixture := newDesktopFixture(t)
	fixture.givenCrontabAlwaysReadable()
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock.Anything).
		Return(desktopRun("run-1", 0), nil)

	status, err := fixture.desktopApplication.Refresh()

	require.NoError(t, err)
	assert.Equal(t, string(entity.StatusIndicatorNormal), status.Indicator)
	require.Len(t, status.Lines, 1)
	assert.Equal(t, status, fixture.desktopApplication.Snapshot(),
		"the menu bar reads the snapshot, so it must be what the refresh just produced")
}

// 只有壞消息值得打擾。成功的執行不通知。
func TestDesktopApplicationDoesNotNotifyOnSuccess(t *testing.T) {
	fixture := newDesktopFixture(t)
	fixture.givenCrontabAlwaysReadable()
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock.Anything).
		Return(desktopRun("run-1", 0), nil).Once()
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock.Anything).
		Return(desktopRun("run-2", 0), nil).Once()

	_, firstErr := fixture.desktopApplication.Refresh()
	_, secondErr := fixture.desktopApplication.Refresh()

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	fixture.notifications.AssertNotCalled(t, "Notify")
}

// 應用開著的期間新出現的失敗，才是值得跳出來的事。
func TestDesktopApplicationNotifiesAFailureThatAppearsWhileItIsRunning(t *testing.T) {
	fixture := newDesktopFixture(t)
	fixture.givenCrontabAlwaysReadable()
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock.Anything).
		Return(desktopRun("run-1", 0), nil).Once()
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock.Anything).
		Return(desktopRun("run-2", 3), nil).Once()
	fixture.notifications.EXPECT().
		Notify(mock.MatchedBy(func(title string) bool { return strings.Contains(title, "Nightly backup") }), mock.Anything).Return(nil).Once()

	_, firstErr := fixture.desktopApplication.Refresh()
	require.NoError(t, firstErr)

	status, err := fixture.desktopApplication.Refresh()

	require.NoError(t, err)
	assert.Equal(t, string(entity.StatusIndicatorAttention), status.Indicator)
}

// 通知管道壞掉不該讓選單列跟著瞎掉：畫面資料照樣可用，錯誤另外回報。
func TestDesktopApplicationStillReportsStatusWhenANotificationCannotBeDelivered(t *testing.T) {
	fixture := newDesktopFixture(t)
	fixture.givenCrontabAlwaysReadable()
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock.Anything).
		Return(desktopRun("run-1", 0), nil).Once()
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock.Anything).
		Return(desktopRun("run-2", 3), nil).Once()
	fixture.notifications.EXPECT().Notify(mock.Anything, mock.Anything).
		Return(errors.New("osascript: not found")).Once()

	_, firstErr := fixture.desktopApplication.Refresh()
	require.NoError(t, firstErr)

	status, err := fixture.desktopApplication.Refresh()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "osascript")
	assert.Equal(t, string(entity.StatusIndicatorAttention), status.Indicator,
		"the picture must stay usable even when the notification channel is broken")
	require.Len(t, status.Lines, 1)
}

// 讀不到來源時什麼都不通知 —— 此時我們對現況一無所知，發通知等於瞎猜。
func TestDesktopApplicationSaysNothingWhenItCannotReadAnything(t *testing.T) {
	fixture := newDesktopFixture(t)
	fixture.crontabRepository.EXPECT().Load().
		Return(nil, "", errors.New("crontab: permission denied"))

	status, err := fixture.desktopApplication.Refresh()

	require.NoError(t, err)
	assert.Equal(t, string(entity.StatusIndicatorUnavailable), status.Indicator)
	assert.Contains(t, status.UnavailableReason, "permission denied")
	fixture.notifications.AssertNotCalled(t, "Notify")
}
