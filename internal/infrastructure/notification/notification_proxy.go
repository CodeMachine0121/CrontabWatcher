package notification

import (
	"fmt"
	"os/exec"
	"strings"
)

// DefaultOsascriptPath 是 macOS 上 osascript 的位置。
const DefaultOsascriptPath = "/usr/bin/osascript"

// notificationScript 是送出通知的 AppleScript。
//
// **文字一律經由 argv 傳入，絕不拼接進腳本字串。** job 名稱來自使用者自己的
// crontab，看似無害，但一個含引號的名稱就足以讓腳本被改寫成別的東西。這條紀律
// 與專案其他地方對待使用者指令的方式一致：原樣交給對方，不自己組。
var notificationScript = []string{
	"on run argv",
	"display notification (item 2 of argv) with title (item 1 of argv)",
	"end run",
}

// NotificationProxy 以 macOS 內建的 osascript 送出系統通知。
//
// 選它而不是引入一個通知用的相依：這個服務常跑在沒有對外網路的機器上，而
// osascript 是系統自帶的。代價是通知橫幅上的來源會顯示為腳本執行器而不是
// cronwatch —— 那是外觀，換到的是零額外相依。
type NotificationProxy struct {
	osascriptPath string
}

// NewNotificationProxy 建立 proxy。osascriptPath 可覆寫，測試才有辦法在不真的
// 對使用者跳通知的前提下驗證參數怎麼傳。
func NewNotificationProxy(osascriptPath string) *NotificationProxy {
	if strings.TrimSpace(osascriptPath) == "" {
		osascriptPath = DefaultOsascriptPath
	}

	return &NotificationProxy{osascriptPath: osascriptPath}
}

// Notify 送出一則通知。
func (proxy *NotificationProxy) Notify(title string, body string) error {
	arguments := make([]string, 0, len(notificationScript)*2+3)
	for _, scriptLine := range notificationScript {
		arguments = append(arguments, "-e", scriptLine)
	}
	arguments = append(arguments, "--", title, body)

	command := exec.Command(proxy.osascriptPath, arguments...)

	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("could not deliver notification via %s: %w: %s",
			proxy.osascriptPath, err, strings.TrimSpace(string(output)))
	}

	return nil
}
