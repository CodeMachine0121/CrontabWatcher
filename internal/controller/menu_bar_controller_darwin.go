//go:build darwin

package controller

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/energye/systray"

	"github.com/james-hsueh/crontab-watcher/internal/application"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
)

// MenuBarController 讓桌面應用進駐選單列。
//
// 它是整個功能唯一無法用單元測試涵蓋的地方，因此裡面刻意沒有任何判斷：該畫什麼
// 由 MenuBarViewModel 決定，該不該通知由領域層決定。這裡只剩下「把字放上去」與
// 「把點擊轉成一次開窗」。
type MenuBarController struct {
	desktopApplication *application.DesktopApplication
	windowProxy        interfaces.IDesktopWindowProxy

	baseURL         string
	refreshInterval time.Duration
	summaryCapacity int

	lineItems     []*systray.MenuItem
	lineJobIDs    []string
	overflowItem  *systray.MenuItem
	emptyItem     *systray.MenuItem
	openItem      *systray.MenuItem
	quitItem      *systray.MenuItem
	stopRefreshes chan struct{}
}

// NewMenuBarController 建立 controller。
//
// baseURL 必須是實際監聽到的位址 —— 桌面形態用的是系統配的臨時埠，組態上那個
// ":0" 不是真的埠號。
func NewMenuBarController(
	desktopApplication *application.DesktopApplication,
	windowProxy interfaces.IDesktopWindowProxy,
	baseURL string,
	refreshInterval time.Duration,
	summaryCapacity int,
) *MenuBarController {
	return &MenuBarController{
		desktopApplication: desktopApplication,
		windowProxy:        windowProxy,
		baseURL:            strings.TrimSuffix(baseURL, "/"),
		refreshInterval:    refreshInterval,
		summaryCapacity:    summaryCapacity,
		stopRefreshes:      make(chan struct{}),
	}
}

// Run 進駐選單列並阻塞直到使用者結束應用。
//
// 必須從主執行緒呼叫：macOS 的選單列綁在主 run loop 上。這也是完整視窗得跑在
// 另一個程序的原因 —— 那邊同樣要一個主 run loop。
func (controller *MenuBarController) Run() {
	systray.Run(controller.onReady, controller.onExit)
}

// onReady 建立選單，並開始定期重新確認現況。
func (controller *MenuBarController) onReady() {
	systray.SetTitle(indicatorTitleNormal)
	systray.SetTooltip("crontab-watcher")

	// 選單項目在 macOS 上無法動態增刪，因此一次建滿上限再靠顯示／隱藏切換。
	controller.lineItems = make([]*systray.MenuItem, 0, controller.summaryCapacity)
	controller.lineJobIDs = make([]string, controller.summaryCapacity)

	for index := 0; index < controller.summaryCapacity; index++ {
		item := systray.AddMenuItem("", "")
		item.Hide()

		lineIndex := index
		item.Click(func() { controller.openWindowForLine(lineIndex) })

		controller.lineItems = append(controller.lineItems, item)
	}

	controller.emptyItem = systray.AddMenuItem("", "")
	controller.emptyItem.Disable()
	controller.emptyItem.Hide()

	controller.overflowItem = systray.AddMenuItem("", "")
	controller.overflowItem.Click(func() { controller.openWindow("/") })
	controller.overflowItem.Hide()

	systray.AddSeparator()

	controller.openItem = systray.AddMenuItem("開啟完整視窗…", "在獨立視窗中檢視與操作全部排程")
	controller.openItem.Click(func() { controller.openWindow("/") })

	controller.quitItem = systray.AddMenuItem("結束 cronwatch", "排程不受影響，仍由系統照常執行")
	controller.quitItem.Click(func() { systray.Quit() })

	controller.refreshNow()

	// 把選單真正掛到狀態列項目上。
	//
	// **少了這一步，圖示會出現但點下去毫無反應。** 這個 systray fork 刻意沒有預設
	// 掛上選單（它把 `[statusItem setMenu:menu]` 那行註解掉了），改成讓使用者自己
	// 決定要選單還是要滑鼠事件回呼。我們要的就是一個正常的下拉選單，不需要滑鼠
	// 事件，所以在這裡掛上去。
	//
	// 必須在全部項目都建好之後才呼叫：它只在選單尚未掛上時才動作。
	systray.CreateMenu()

	go controller.refreshPeriodically()
}

// onExit 收掉視窗與輪詢。排程本身完全不受影響 —— 這個應用從來就不是排程器。
func (controller *MenuBarController) onExit() {
	close(controller.stopRefreshes)
	controller.windowProxy.Close()
}

// refreshPeriodically 定期重新確認現況。
func (controller *MenuBarController) refreshPeriodically() {
	ticker := time.NewTicker(controller.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-controller.stopRefreshes:
			return
		case <-ticker.C:
			controller.refreshNow()
		}
	}
}

// refreshNow 重新確認一次並重畫選單。
//
// 讀取在這條背景路徑上進行，選單展開時讀的是快照，所以一次慢讀的後果只是畫面
// 舊了幾秒，而不是選單卡住。
func (controller *MenuBarController) refreshNow() {
	status, err := controller.desktopApplication.Refresh()
	if err != nil {
		// 通知送不出去不影響畫面，但要留下痕跡 —— 靜靜地失敗會讓使用者以為
		// 從來沒有 job 出過事。
		log.Printf("could not deliver a failure notification: %v", err)
	}

	controller.render(NewMenuBarViewModel(status))
}

// render 把 view model 畫到選單上。
func (controller *MenuBarController) render(viewModel MenuBarViewModel) {
	systray.SetTitle(viewModel.IndicatorTitle)
	systray.SetTooltip(viewModel.Tooltip)

	for index, item := range controller.lineItems {
		if index < len(viewModel.LineTitles) {
			item.SetTitle(viewModel.LineTitles[index])
			controller.lineJobIDs[index] = viewModel.LineJobIDs[index]
			item.Show()

			continue
		}

		controller.lineJobIDs[index] = ""
		item.Hide()
	}

	controller.setItemText(controller.emptyItem, viewModel.EmptyMessage)
	controller.setItemText(controller.overflowItem, viewModel.OverflowTitle)
}

// setItemText 有字就顯示，沒字就隱藏。
func (controller *MenuBarController) setItemText(item *systray.MenuItem, text string) {
	if text == "" {
		item.Hide()

		return
	}

	item.SetTitle(text)
	item.Show()
}

// openWindowForLine 開啟某一筆排程的詳情。
func (controller *MenuBarController) openWindowForLine(lineIndex int) {
	if lineIndex >= len(controller.lineJobIDs) || controller.lineJobIDs[lineIndex] == "" {
		return
	}

	controller.openWindow(fmt.Sprintf("/jobs/%s/detail", controller.lineJobIDs[lineIndex]))
}

// openWindow 讓完整視窗顯示某個頁面。已經開著時重用它，不另開第二個。
func (controller *MenuBarController) openWindow(path string) {
	if err := controller.windowProxy.Open(controller.baseURL + path); err != nil {
		log.Printf("could not open the window: %v", err)
	}
}

// Supported 回報這個平台有沒有桌面形態。macOS 上永遠有。
func (controller *MenuBarController) Supported() error {
	return nil
}
