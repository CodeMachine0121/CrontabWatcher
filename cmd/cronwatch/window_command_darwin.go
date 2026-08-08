//go:build darwin

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/webview/webview_go"
)

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

		window.Dispatch(func() { window.Navigate(targetURL) })
	}

	window.Terminate()
}
