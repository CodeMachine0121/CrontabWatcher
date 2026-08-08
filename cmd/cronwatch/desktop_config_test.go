package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesktopConfigurationDefaults(t *testing.T) {
	configuration := loadServerConfiguration()

	assert.Equal(t, defaultDesktopRefreshInterval, configuration.DesktopRefreshInterval)
	assert.Equal(t, defaultDesktopSummaryLineLimit, configuration.DesktopSummaryLineLimit)
}

func TestDesktopConfigurationReadsOverrides(t *testing.T) {
	t.Setenv("DESKTOP_REFRESH_INTERVAL_SECONDS", "60")
	t.Setenv("DESKTOP_SUMMARY_LINE_LIMIT", "8")

	configuration := loadServerConfiguration()

	assert.Equal(t, time.Minute, configuration.DesktopRefreshInterval)
	assert.Equal(t, 8, configuration.DesktopSummaryLineLimit)
}

// 打錯的值靜靜地照著跑，只會讓使用者之後對著奇怪的行為百思不解，所以一律修正
// 並說出來。
func TestDesktopConfigurationRaisesValuesThatWouldMisbehave(t *testing.T) {
	t.Setenv("DESKTOP_REFRESH_INTERVAL_SECONDS", "0")
	t.Setenv("DESKTOP_SUMMARY_LINE_LIMIT", "0")

	configuration := loadServerConfiguration()

	assert.Equal(t, minimumDesktopRefreshInterval, configuration.DesktopRefreshInterval)
	assert.Equal(t, 1, configuration.DesktopSummaryLineLimit)
	assert.True(t, containsWarningAbout(configuration.Warnings, "DESKTOP_REFRESH_INTERVAL_SECONDS"))
	assert.True(t, containsWarningAbout(configuration.Warnings, "DESKTOP_SUMMARY_LINE_LIMIT"))
}

// 桌面形態看的必須是使用者真正在用的那份 crontab，而不是容器路徑下那份不存在的
// 檔案 —— 後者會顯示一份空清單，看起來像壞了。
func TestApplyDesktopDefaultsWatchesTheUsersOwnCrontab(t *testing.T) {
	configuration := applyDesktopDefaults(loadServerConfiguration())

	assert.Equal(t, CrontabSourceCommand, configuration.CrontabSource)
	assert.True(t, configuration.UsesUserCrontab())
}

// 桌面形態的服務只給這台機器上的視窗看，因此綁 loopback 並讓系統配一個臨時埠：
// 其他裝置連不進來，也不會跟既有的 8080 打架。
func TestApplyDesktopDefaultsBindsLoopbackOnAnEphemeralPort(t *testing.T) {
	t.Setenv("SERVER_ADDRESS", "0.0.0.0:8080")

	configuration := applyDesktopDefaults(loadServerConfiguration())

	assert.Equal(t, desktopServerAddress, configuration.ServerAddress)
	assert.True(t, strings.HasPrefix(configuration.ServerAddress, "127.0.0.1:"),
		"the desktop window is local, so nothing else may reach it")
	assert.True(t, strings.HasSuffix(configuration.ServerAddress, ":0"),
		"an ephemeral port keeps it out of the way of anything already listening")
}

// 狀態預設放在使用者自己的家目錄下，而不是容器裡的 /data —— 那個路徑在桌面上
// 不存在，寫不進去。
func TestApplyDesktopDefaultsPutsStateUnderTheHomeDirectory(t *testing.T) {
	homeDirectory, err := os.UserHomeDir()
	require.NoError(t, err)

	configuration := applyDesktopDefaults(loadServerConfiguration())

	expectedStateDirectory := filepath.Join(homeDirectory, ".local", "state", "crontab-watcher")
	assert.Equal(t, filepath.Join(expectedStateDirectory, "logs"), configuration.RunLogDirectory)
	assert.Equal(t, filepath.Join(expectedStateDirectory, "runs.jsonl"), configuration.RunRecordFilePath)
	assert.Equal(t, filepath.Join(expectedStateDirectory, "backups"), configuration.CrontabBackupDirectory)
	assert.Equal(t, filepath.Join(expectedStateDirectory, "desktop.lock"), configuration.DesktopLockFilePath)
}

// 明確設定過的路徑不能被覆蓋掉 —— 使用者說了算。
func TestApplyDesktopDefaultsKeepsPathsThatWereSetOnPurpose(t *testing.T) {
	t.Setenv("RUN_LOG_DIRECTORY", "/custom/logs")
	t.Setenv("RUN_RECORD_FILE_PATH", "/custom/runs.jsonl")

	configuration := applyDesktopDefaults(loadServerConfiguration())

	assert.Equal(t, "/custom/logs", configuration.RunLogDirectory)
	assert.Equal(t, "/custom/runs.jsonl", configuration.RunRecordFilePath)
}

func containsWarningAbout(warnings []string, variableName string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, variableName) {
			return true
		}
	}

	return false
}
