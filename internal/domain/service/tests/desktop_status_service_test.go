package service_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/domain/interface/mocks"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
)

const desktopSummaryLineLimit = 20

const desktopCrontab = "# cronwatch:id=job-1\n" +
	"# cronwatch:description=Nightly backup\n" +
	"0 3 * * * /app/cronwatch run --job=job-1 -- /backup.sh\n" +
	"0 5 * * * /clean.sh\n"

type desktopStatusServiceFixture struct {
	service           *service.DesktopStatusService
	crontabRepository *mocks.MockICrontabDocumentRepository
	jobRunRepository  *mocks.MockIJobRunRepository
}

func newDesktopStatusServiceFixture(t *testing.T) desktopStatusServiceFixture {
	t.Helper()

	crontabRepository := mocks.NewMockICrontabDocumentRepository(t)
	jobRunRepository := mocks.NewMockIJobRunRepository(t)

	return desktopStatusServiceFixture{
		service: service.NewDesktopStatusService(
			crontabRepository, jobRunRepository, desktopSummaryLineLimit),
		crontabRepository: crontabRepository,
		jobRunRepository:  jobRunRepository,
	}
}

func finishedRun(runID string, jobID string, exitCode int) *entity.JobRun {
	run := entity.NewJobRun(runID, jobID, entity.TriggerSourceSchedule, referenceNow)
	run.Finish(referenceNow, exitCode, false, "")

	return run
}

func TestDesktopStatusServiceSummarisesWhatTheMenuBarNeeds(t *testing.T) {
	fixture := newDesktopStatusServiceFixture(t)
	fixture.crontabRepository.EXPECT().Load().
		Return(entity.ParseCrontabDocument(desktopCrontab), "fingerprint", nil)
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock_anySlice()).
		Return(map[string]*entity.JobRun{"job-1": finishedRun("run-1", "job-1", 0)}, nil)

	refresh := fixture.service.RefreshDesktopStatus(referenceNow)

	assert.Equal(t, string(entity.StatusIndicatorNormal), refresh.Status.Indicator)
	assert.Empty(t, refresh.Status.UnavailableReason)
	require.Len(t, refresh.Status.Lines, 2)
	assert.Zero(t, refresh.Status.OmittedLineCount)
	assert.Empty(t, refresh.NewFailureNotices, "the first look never announces anything")

	lineByJobID := map[string]string{}
	for _, line := range refresh.Status.Lines {
		lineByJobID[line.JobID] = line.Outcome
	}
	assert.Equal(t, string(entity.LatestRunOutcomeSucceeded), lineByJobID["job-1"])
}

// 讀不到 crontab 不是例外而是要顯示出來的事實，因此它走的是同一條回傳路徑，
// 而不是一個呼叫方可能忘記處理的錯誤。
func TestDesktopStatusServiceReportsAnUnreadableCrontabAsAState(t *testing.T) {
	fixture := newDesktopStatusServiceFixture(t)
	fixture.crontabRepository.EXPECT().Load().
		Return(nil, "", errors.New("crontab: permission denied"))

	refresh := fixture.service.RefreshDesktopStatus(referenceNow)

	assert.Equal(t, string(entity.StatusIndicatorUnavailable), refresh.Status.Indicator)
	assert.Contains(t, refresh.Status.UnavailableReason, "permission denied")
	assert.Empty(t, refresh.Status.Lines)
	assert.Empty(t, refresh.NewFailureNotices)
}

// 讀得到 crontab 但讀不到執行紀錄，一樣什麼都答不準。降級成「沒有紀錄」會讓一個
// 實際上跑失敗的 job 看起來只是還沒跑過。
func TestDesktopStatusServiceReportsUnreadableRunRecordsAsAState(t *testing.T) {
	fixture := newDesktopStatusServiceFixture(t)
	fixture.crontabRepository.EXPECT().Load().
		Return(entity.ParseCrontabDocument(desktopCrontab), "fingerprint", nil)
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock_anySlice()).
		Return(nil, errors.New("runs.jsonl is corrupt"))

	refresh := fixture.service.RefreshDesktopStatus(referenceNow)

	assert.Equal(t, string(entity.StatusIndicatorUnavailable), refresh.Status.Indicator)
	assert.Contains(t, refresh.Status.UnavailableReason, "runs.jsonl")
	assert.Empty(t, refresh.Status.Lines)
}

// 服務記得上一輪看過什麼，所以第二輪才出現的失敗會被挑出來，而且只挑一次。
func TestDesktopStatusServiceAnnouncesAFailureThatAppearsBetweenRefreshes(t *testing.T) {
	fixture := newDesktopStatusServiceFixture(t)
	fixture.crontabRepository.EXPECT().Load().
		Return(entity.ParseCrontabDocument(desktopCrontab), "fingerprint", nil)
	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock_anySlice()).
		Return(map[string]*entity.JobRun{"job-1": finishedRun("run-1", "job-1", 0)}, nil).Once()

	require.Empty(t, fixture.service.RefreshDesktopStatus(referenceNow).NewFailureNotices)

	fixture.jobRunRepository.EXPECT().LatestByJobIDs(mock_anySlice()).
		Return(map[string]*entity.JobRun{"job-1": finishedRun("run-2", "job-1", 3)}, nil)

	refresh := fixture.service.RefreshDesktopStatus(referenceNow)

	require.Len(t, refresh.NewFailureNotices, 1)
	assert.Equal(t, "run-2", refresh.NewFailureNotices[0].RunID)
	assert.Equal(t, "job-1", refresh.NewFailureNotices[0].JobID)
	assert.Equal(t, string(entity.FailureKindFailed), refresh.NewFailureNotices[0].Kind)
	assert.Contains(t, refresh.NewFailureNotices[0].Title, "Nightly backup")
	assert.NotEmpty(t, refresh.NewFailureNotices[0].Body)
	assert.Equal(t, string(entity.StatusIndicatorAttention), refresh.Status.Indicator)

	assert.Empty(t, fixture.service.RefreshDesktopStatus(referenceNow).NewFailureNotices,
		"the same failure must not be announced twice")
}
