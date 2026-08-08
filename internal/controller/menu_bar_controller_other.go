//go:build !darwin

package controller

import (
	"errors"
	"runtime"
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/application"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
)

// ErrDesktopUnsupportedPlatform 表示這個平台沒有桌面形態。
//
// 不支援是在**編譯期**分流的，因此容器建置（GOOS=linux、CGO_ENABLED=0）根本不會
// 碰到選單列的相依。使用者拿到的是一句話說清楚的訊息，而不是一個莫名其妙的失敗。
var ErrDesktopUnsupportedPlatform = errors.New(
	"the desktop mode is only available on macOS; on " + runtime.GOOS +
		" use `cronwatch serve` and open the page in a browser")

// MenuBarController 在非 macOS 平台上只負責說清楚這裡沒有選單列。
type MenuBarController struct{}

// NewMenuBarController 建立一個什麼都不做的 controller。
func NewMenuBarController(
	_ *application.DesktopApplication,
	_ interfaces.IDesktopWindowProxy,
	_ string,
	_ time.Duration,
	_ int,
) *MenuBarController {
	return &MenuBarController{}
}

// Run 立刻返回。呼叫方會在啟動前就先問過 Supported，這裡只是最後一道防線。
func (controller *MenuBarController) Run() {}

// Supported 回報這個平台有沒有桌面形態。
func (controller *MenuBarController) Supported() error {
	return ErrDesktopUnsupportedPlatform
}
