//go:build !darwin

package main

import (
	"fmt"
	"os"

	"github.com/james-hsueh/crontab-watcher/internal/controller"
)

// runWindowCommand 在非 macOS 平台上只說清楚這裡沒有視窗。
//
// 不支援在編譯期就分流，所以容器建置根本不會編到視窗的相依。
func runWindowCommand(_ []string) int {
	fmt.Fprintf(os.Stderr, "cronwatch window: %v\n", controller.ErrDesktopUnsupportedPlatform)

	return usageExitCode
}
