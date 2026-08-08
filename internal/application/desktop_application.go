package application

import (
	"errors"
	"sync"

	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
)

// DesktopApplication 編排桌面形態的一次刷新：問一次現況、把該通知的送出去、
// 把畫面資料留下來給選單列取用。
type DesktopApplication struct {
	desktopStatusService *service.DesktopStatusService
	clock                interfaces.IClock
	notificationProxy    interfaces.INotificationProxy

	// snapshot 是最近一次刷新的結果。選單列展開時讀它而不是重新讀一次檔案：
	// 點開的瞬間卡住，比看到幾秒前的資料糟得多。
	snapshotMutex sync.RWMutex
	snapshot      dto.DesktopStatusDto
}

// NewDesktopApplication 建立 application。
//
// 初始快照是「正常且空的」：選單列在第一次刷新完成前的那一瞬間總得畫點什麼，
// 而那一瞬間我們確實還不知道有沒有事要理。
func NewDesktopApplication(
	desktopStatusService *service.DesktopStatusService,
	clock interfaces.IClock,
	notificationProxy interfaces.INotificationProxy,
) *DesktopApplication {
	return &DesktopApplication{
		desktopStatusService: desktopStatusService,
		clock:                clock,
		notificationProxy:    notificationProxy,
		snapshot: dto.DesktopStatusDto{
			Indicator: string(initialIndicator),
			Lines:     []dto.JobStatusLineDto{},
		},
	}
}

// initialIndicator 是還沒問過任何事情之前的圖示狀態。
const initialIndicator = "normal"

// Refresh 重新確認一次現況，送出這一輪新出現的失敗通知，並更新快照。
//
// 回傳的狀態**永遠可用**；error 只代表「有通知沒送出去」，與畫面資料無關。把它
// 分開是刻意的：通知管道壞掉不該讓選單列跟著瞎掉。記錄由最外層負責，這一層不
// 自己寫 log。
func (application *DesktopApplication) Refresh() (dto.DesktopStatusDto, error) {
	refresh := application.desktopStatusService.RefreshDesktopStatus(application.clock.Now())

	application.snapshotMutex.Lock()
	application.snapshot = refresh.Status
	application.snapshotMutex.Unlock()

	deliveryErrors := make([]error, 0, len(refresh.NewFailureNotices))
	for _, notice := range refresh.NewFailureNotices {
		if err := application.notificationProxy.Notify(notice.Title, notice.Body); err != nil {
			deliveryErrors = append(deliveryErrors, err)
		}
	}

	return refresh.Status, errors.Join(deliveryErrors...)
}

// Snapshot 回傳最近一次刷新的結果，不重新讀取。
func (application *DesktopApplication) Snapshot() dto.DesktopStatusDto {
	application.snapshotMutex.RLock()
	defer application.snapshotMutex.RUnlock()

	return application.snapshot
}
