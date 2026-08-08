package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/desktop"
)

// 測試用一個假的「視窗」：它把啟動參數記下來，然後把 stdin 收到的每一行也記下來，
// 直到 stdin 關閉為止。真正的視窗做的事一模一樣，只是還會畫出來。
func writeFakeWindowBinary(t *testing.T) (executablePath string, recordPath string) {
	t.Helper()

	directory := t.TempDir()
	executablePath = filepath.Join(directory, "fake-cronwatch")
	recordPath = filepath.Join(directory, "window.log")

	script := "#!/bin/sh\n" +
		"printf 'start %s\\n' \"$2\" >> " + recordPath + "\n" +
		"while IFS= read -r line; do printf 'navigate %s\\n' \"$line\" >> " + recordPath + "; done\n"

	require.NoError(t, os.WriteFile(executablePath, []byte(script), 0o755))

	return executablePath, recordPath
}

func eventuallyRecorded(t *testing.T, recordPath string, expectedLines int) []string {
	t.Helper()

	var lines []string
	require.Eventually(t, func() bool {
		contentBytes, err := os.ReadFile(recordPath)
		if err != nil {
			return false
		}

		lines = strings.Split(strings.TrimSuffix(string(contentBytes), "\n"), "\n")

		return len(lines) >= expectedLines
	}, 3*time.Second, 20*time.Millisecond)

	return lines
}

func TestDesktopWindowProxyStartsTheWindowWithTheRequestedAddress(t *testing.T) {
	executablePath, recordPath := writeFakeWindowBinary(t)

	proxy := desktop.NewDesktopWindowProxy(executablePath)
	t.Cleanup(proxy.Close)

	require.NoError(t, proxy.Open("http://127.0.0.1:51234/jobs/job-1/detail"))

	lines := eventuallyRecorded(t, recordPath, 1)
	assert.Equal(t, "start --url=http://127.0.0.1:51234/jobs/job-1/detail", lines[0])
}

// 已經開著的時候不再開第二個 —— 桌面上冒出兩個一模一樣的視窗，是使用者最不想
// 看到的。既有的那個收到新網址就好。
func TestDesktopWindowProxyReusesAWindowThatIsAlreadyOpen(t *testing.T) {
	executablePath, recordPath := writeFakeWindowBinary(t)

	proxy := desktop.NewDesktopWindowProxy(executablePath)
	t.Cleanup(proxy.Close)

	require.NoError(t, proxy.Open("http://127.0.0.1:51234/"))
	eventuallyRecorded(t, recordPath, 1)

	require.NoError(t, proxy.Open("http://127.0.0.1:51234/jobs/job-2/detail"))

	lines := eventuallyRecorded(t, recordPath, 2)
	require.Len(t, lines, 2, "a second window must not be started")
	assert.Equal(t, "start --url=http://127.0.0.1:51234/", lines[0])
	assert.Equal(t, "navigate http://127.0.0.1:51234/jobs/job-2/detail", lines[1])
}

// 視窗被使用者關掉之後，下一次點擊要能重新開一個，而不是對著一個死掉的程序
// 寫東西然後假裝成功了。
func TestDesktopWindowProxyStartsAgainAfterTheWindowIsGone(t *testing.T) {
	executablePath, recordPath := writeFakeWindowBinary(t)

	proxy := desktop.NewDesktopWindowProxy(executablePath)
	t.Cleanup(proxy.Close)

	require.NoError(t, proxy.Open("http://127.0.0.1:51234/"))
	eventuallyRecorded(t, recordPath, 1)

	proxy.Close()

	require.NoError(t, proxy.Open("http://127.0.0.1:51234/jobs/job-3/detail"))

	lines := eventuallyRecorded(t, recordPath, 2)
	assert.Equal(t, "start --url=http://127.0.0.1:51234/jobs/job-3/detail", lines[1],
		"the second call must start a new window, not write into a dead one")
}

func TestDesktopWindowProxyReportsAnExecutableItCannotStart(t *testing.T) {
	proxy := desktop.NewDesktopWindowProxy(filepath.Join(t.TempDir(), "absent"))
	t.Cleanup(proxy.Close)

	require.Error(t, proxy.Open("http://127.0.0.1:51234/"))
}

func TestDesktopWindowProxyCloseIsSafeWhenNothingIsOpen(t *testing.T) {
	proxy := desktop.NewDesktopWindowProxy(filepath.Join(t.TempDir(), "absent"))

	assert.NotPanics(t, proxy.Close)
	assert.NotPanics(t, proxy.Close)
}
