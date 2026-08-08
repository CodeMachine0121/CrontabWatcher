//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// cronwatchBecomeForegroundWindow 讓視窗子程序成為一個正常的前景 app。
//
// 為什麼必須自己來：整個 app bundle 標了 LSUIElement（選單列 app 不該佔一個
// Dock 圖示），而子程序用的是同一份 Info.plist，於是它預設也是 accessory ——
// accessory 的視窗開起來會躲在別人後面、拿不到鍵盤焦點，而這個視窗裡有要填的
// 表單。webview 只在「沒有被包成 bundle」時才自己處理這件事，包起來之後就得由
// 我們宣告。
//
// 這也是「視窗已經開著時把它帶到最前」的實作。先前的做法是叫 osascript 去操作
// System Events，那需要輔助使用權限，拿不到就只能安靜地失敗。這裡是程序對自己
// 說話，不需要任何權限。
static void cronwatchBecomeForegroundWindow(void) {
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
    [NSApp activateIgnoringOtherApps:YES];
}
*/
import "C"

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/webview/webview_go"
)

// macOS 的 NSApplication 必須跑在主執行緒上，而 Go 只保證 main 從主執行緒**開始**
// ——它之後可能被排到別的執行緒去。沒有這一行，視窗與選單列都會在啟動時死於
// 「NSApp with wrong _running count」。
//
// 對其他子命令沒有副作用：一個伺服器不在乎自己的 main goroutine 綁在哪個執行緒。
func init() {
	runtime.LockOSThread()
}

const windowUsage = `usage: cronwatch window --url=<address>

Opens the standalone window. Started by the menu bar app; you do not normally
run this yourself. Further addresses may be sent one per line on stdin.`

const (
	windowTitle  = "crontab-watcher"
	windowWidth  = 1040
	windowHeight = 760
)

// runWindowCommand 開啟完整視窗。
//
// 它是一個獨立的子命令而不是選單列裡的一條 goroutine，因為選單列與視窗兩邊的
// GUI 函式庫都要求佔用 macOS 的主 run loop。分成兩個程序之後各自有一個。
func runWindowCommand(arguments []string) int {
	targetURL, err := parseWindowArguments(arguments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cronwatch window: %v\n\n%s\n", err, windowUsage)

		return usageExitCode
	}

	C.cronwatchBecomeForegroundWindow()

	window := webview.New(false)
	defer window.Destroy()

	window.SetTitle(windowTitle)
	window.SetSize(windowWidth, windowHeight, webview.HintNone)
	window.Navigate(targetURL)

	// 父程序用 stdin 送來後續要看的網址。stdin 關閉即代表父程序不在了，視窗
	// 跟著收掉 —— 留一個沒有選單列的孤兒視窗只會讓人困惑。
	go followNavigationRequests(window, os.Stdin)

	window.Run()

	return 0
}

// parseWindowArguments 取出 --url。
func parseWindowArguments(arguments []string) (string, error) {
	for _, argument := range arguments {
		if targetURL, found := strings.CutPrefix(argument, "--url="); found {
			if strings.TrimSpace(targetURL) == "" {
				return "", fmt.Errorf("--url is empty")
			}

			return targetURL, nil
		}
	}

	return "", fmt.Errorf("--url is required")
}

// followNavigationRequests 讀取父程序送來的網址，一行一個。
//
// 導覽必須回到主執行緒上執行，因此經由 Dispatch —— 直接從這條 goroutine 動視窗
// 會踩到 Cocoa 的執行緒規則。
func followNavigationRequests(window webview.WebView, input io.Reader) {
	scanner := bufio.NewScanner(input)

	for scanner.Scan() {
		targetURL := strings.TrimSpace(scanner.Text())
		if targetURL == "" {
			continue
		}

		window.Dispatch(func() {
			window.Navigate(targetURL)
			C.cronwatchBecomeForegroundWindow()
		})
	}

	window.Terminate()
}
