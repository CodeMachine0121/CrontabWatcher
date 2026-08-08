package notification_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/notification"
)

// 測試用一個假的 osascript：它把收到的每一個參數各寫一行。這樣才驗得出「文字是
// 以參數傳進去的」，而不是被拼進腳本裡 —— 後者正是我們要避免的形態，也是真的
// 對使用者跳通知所無法驗證的東西。
func writeRecordingOsascript(t *testing.T, exitCode int, message string) (scriptPath string, recordPath string) {
	t.Helper()

	directory := t.TempDir()
	scriptPath = filepath.Join(directory, "osascript")
	recordPath = filepath.Join(directory, "arguments.txt")

	script := "#!/bin/sh\n" +
		": > " + recordPath + "\n" +
		"for argument in \"$@\"; do printf '%s\\n' \"$argument\" >> " + recordPath + "; done\n"
	if message != "" {
		script += "printf '%s\\n' '" + message + "' >&2\n"
	}
	script += "exit " + strconv.Itoa(exitCode) + "\n"

	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	return scriptPath, recordPath
}

func recordedArguments(t *testing.T, recordPath string) []string {
	t.Helper()

	contentBytes, err := os.ReadFile(recordPath)
	require.NoError(t, err)

	return strings.Split(strings.TrimSuffix(string(contentBytes), "\n"), "\n")
}

func TestNotificationProxyPassesTitleAndBodyAsArguments(t *testing.T) {
	scriptPath, recordPath := writeRecordingOsascript(t, 0, "")

	err := notification.NewNotificationProxy(scriptPath).
		Notify("排程失敗：Nightly backup", "結束碼 3")

	require.NoError(t, err)

	arguments := recordedArguments(t, recordPath)
	require.GreaterOrEqual(t, len(arguments), 3)
	assert.Equal(t, "--", arguments[len(arguments)-3],
		"everything after -- is data, never script")
	assert.Equal(t, "排程失敗：Nightly backup", arguments[len(arguments)-2])
	assert.Equal(t, "結束碼 3", arguments[len(arguments)-1])
}

// 一個含引號的 job 名稱不能有機會改寫腳本。名稱來自使用者自己的 crontab，
// 但「來自使用者自己」不是把它拼進指令的理由。
func TestNotificationProxyKeepsQuotesInTheTextHarmless(t *testing.T) {
	scriptPath, recordPath := writeRecordingOsascript(t, 0, "")

	hostileTitle := `排程失敗："; do shell script "rm -rf /"; "`

	err := notification.NewNotificationProxy(scriptPath).Notify(hostileTitle, "結束碼 1")

	require.NoError(t, err)

	arguments := recordedArguments(t, recordPath)
	assert.Equal(t, hostileTitle, arguments[len(arguments)-2],
		"the text must arrive verbatim as one argument, not as script to run")

	for _, argument := range arguments[:len(arguments)-2] {
		assert.NotContains(t, argument, "rm -rf",
			"no part of the hostile text may end up inside the script itself")
	}
}

// 送不出去要說出來。靜靜地失敗會讓使用者以為沒有任何 job 出過事。
func TestNotificationProxyReportsADeliveryFailure(t *testing.T) {
	scriptPath, _ := writeRecordingOsascript(t, 1, "execution error: Not authorised")

	err := notification.NewNotificationProxy(scriptPath).Notify("title", "body")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Not authorised")
}

func TestNotificationProxyReportsAMissingCommand(t *testing.T) {
	err := notification.NewNotificationProxy(filepath.Join(t.TempDir(), "absent")).
		Notify("title", "body")

	require.Error(t, err)
}

func TestNotificationProxyFallsBackToTheSystemPath(t *testing.T) {
	assert.NotNil(t, notification.NewNotificationProxy("   "))
	assert.NotEmpty(t, notification.DefaultOsascriptPath)
}
