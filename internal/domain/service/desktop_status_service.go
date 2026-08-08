package service

import (
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
)

// DesktopStatusService 是桌面形態的唯一入口。
//
// **它是本專案唯一有狀態的 domain service**：它持有一份失敗通知的帳，因為「哪些
// 失敗已經通知過」本質上是一段跨呼叫的記憶，而那段記憶屬於領域層（application
// 不碰 entity）。一個實例對應桌面應用的一次執行期間。
type DesktopStatusService struct {
	crontabDocumentRepository interfaces.ICrontabDocumentRepository
	jobRunRepository          interfaces.IJobRunRepository
	failureNoticeLedger       *entity.FailureNoticeLedger
	summaryLineLimit          int
}

// NewDesktopStatusService 建立 service。
func NewDesktopStatusService(
	crontabDocumentRepository interfaces.ICrontabDocumentRepository,
	jobRunRepository interfaces.IJobRunRepository,
	summaryLineLimit int,
) *DesktopStatusService {
	return &DesktopStatusService{
		crontabDocumentRepository: crontabDocumentRepository,
		jobRunRepository:          jobRunRepository,
		failureNoticeLedger:       entity.NewFailureNoticeLedger(),
		summaryLineLimit:          summaryLineLimit,
	}
}

// RefreshDesktopStatus 重新確認一次現況，回傳該畫什麼與該通知什麼。
//
// **刻意不回傳 error。** 讀不到來源在這個用例裡不是例外而是一種要顯示出來的業務
// 狀態；若簽章帶 error，呼叫方就有兩條路可走，而其中一條（忽略錯誤、顯示空清單）
// 正是規格明確禁止的行為。失敗原因放在 Status.UnavailableReason 裡。
//
// 這是本專案唯一一個這樣設計的 domain service 方法，不要比照套用。
func (service *DesktopStatusService) RefreshDesktopStatus(now time.Time) dto.DesktopRefreshDto {
	return service.buildRefresh(service.loadStatus(now))
}

// loadStatus 讀出現況。任何一步讀不到，整份就是「無法取得」——只讀到一半的畫面
// 比沒有畫面更糟，它看起來像現況。
func (service *DesktopStatusService) loadStatus(now time.Time) *entity.DesktopStatus {
	document, _, err := service.crontabDocumentRepository.Load()
	if err != nil {
		return entity.NewUnavailableDesktopStatus(err.Error())
	}

	jobs := document.Jobs()

	jobIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.JobID())
	}

	// 讀不到執行紀錄不降級成「沒有紀錄」：那會讓一個實際上跑失敗的 job 看起來
	// 只是還沒跑過，正是這個功能最不該犯的錯。
	latestRuns, err := service.jobRunRepository.LatestByJobIDs(jobIDs)
	if err != nil {
		return entity.NewUnavailableDesktopStatus(err.Error())
	}

	return entity.NewDesktopStatus(jobs, latestRuns, now)
}

// buildRefresh 把現況攤成 DTO，並與通知帳結算。
func (service *DesktopStatusService) buildRefresh(status *entity.DesktopStatus) dto.DesktopRefreshDto {
	notices := service.failureNoticeLedger.Reconcile(status)
	lines, omittedLineCount := status.Lines(service.summaryLineLimit)

	lineDtos := make([]dto.JobStatusLineDto, 0, len(lines))
	for _, line := range lines {
		lineDtos = append(lineDtos, dto.JobStatusLineDto{
			JobID:               line.JobID,
			DisplayName:         line.DisplayName,
			ScheduleDescription: line.ScheduleDescription,
			NextRunAt:           line.NextRunAt,
			Enabled:             line.Enabled,
			Outcome:             line.Outcome,
			NeedsAttention:      line.NeedsAttention,
		})
	}

	noticeDtos := make([]dto.FailureNoticeDto, 0, len(notices))
	for _, notice := range notices {
		noticeDtos = append(noticeDtos, dto.FailureNoticeDto{
			RunID: notice.RunID(),
			JobID: notice.JobID(),
			Kind:  string(notice.Kind()),
			Title: notice.NotificationTitle(),
			Body:  notice.NotificationBody(),
		})
	}

	return dto.DesktopRefreshDto{
		Status: dto.DesktopStatusDto{
			Indicator:         string(status.Indicator()),
			UnavailableReason: status.UnavailableReason(),
			Lines:             lineDtos,
			OmittedLineCount:  omittedLineCount,
		},
		NewFailureNotices: noticeDtos,
	}
}
