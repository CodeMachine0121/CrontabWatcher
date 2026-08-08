package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// defaultDesktopRefreshInterval 是重新確認現況的間隔。
	//
	// 30 秒是「通知夠即時」與「不必要的重複讀取」之間的取捨：排程失敗的處理
	// 通常不是秒級任務，但超過一分鐘就會讓人覺得通知遲鈍。
	defaultDesktopRefreshInterval = 30 * time.Second

	// minimumDesktopRefreshInterval 是下界。比這更頻繁只是在燒 CPU 讀同一份檔案。
	minimumDesktopRefreshInterval = 5 * time.Second

	// defaultDesktopSummaryLineLimit 是選單列摘要最多列幾筆。
	defaultDesktopSummaryLineLimit = 20

	// desktopServerAddress 只綁 loopback，並讓系統配一個臨時埠。
	//
	// 桌面形態的頁面只給這台機器上的視窗看：其他裝置連不進來，也不會跟既有的
	// 8080 打架。這個服務能執行任意指令，等同遠端 shell —— 少開一個口就少一份
	// 風險。
	desktopServerAddress = "127.0.0.1:0"

	// desktopStateDirectoryName 是狀態放在家目錄下的位置，與 make start-host 一致。
	desktopStateDirectoryName = "crontab-watcher"
)

// loadDesktopSettings 讀出桌面形態專屬的兩個設定。
func loadDesktopSettings() (refreshInterval time.Duration, summaryLineLimit int, warnings []string) {
	warnings = make([]string, 0)

	intervalSeconds, warning := parseIntWithDefault(
		"DESKTOP_REFRESH_INTERVAL_SECONDS", int(defaultDesktopRefreshInterval.Seconds()))
	warnings = appendWarning(warnings, warning)

	refreshInterval = time.Duration(intervalSeconds) * time.Second
	if refreshInterval < minimumDesktopRefreshInterval {
		warnings = append(warnings, fmt.Sprintf(
			"DESKTOP_REFRESH_INTERVAL_SECONDS was %d, raised to %d",
			intervalSeconds, int(minimumDesktopRefreshInterval.Seconds())))
		refreshInterval = minimumDesktopRefreshInterval
	}

	summaryLineLimit, warning = parseIntWithDefault(
		"DESKTOP_SUMMARY_LINE_LIMIT", defaultDesktopSummaryLineLimit)
	warnings = appendWarning(warnings, warning)

	if summaryLineLimit < 1 {
		warnings = append(warnings, fmt.Sprintf(
			"DESKTOP_SUMMARY_LINE_LIMIT was %d, raised to 1", summaryLineLimit))
		summaryLineLimit = 1
	}

	return refreshInterval, summaryLineLimit, warnings
}

// applyDesktopDefaults 把一份通用組態調成桌面形態該有的樣子。
//
// 三件事是桌面形態的定義，不是可以商量的預設值：看使用者真正在用的那份 crontab、
// 只綁 loopback、狀態放在家目錄下。其餘凡是使用者明確設定過的，一律尊重。
func applyDesktopDefaults(configuration ServerConfiguration) ServerConfiguration {
	configuration.CrontabSource = CrontabSourceCommand
	configuration.ServerAddress = desktopServerAddress

	stateDirectory := desktopStateDirectory()

	configuration.RunLogDirectory = pathUnlessConfigured(
		"RUN_LOG_DIRECTORY", configuration.RunLogDirectory, filepath.Join(stateDirectory, "logs"))
	configuration.RunRecordFilePath = pathUnlessConfigured(
		"RUN_RECORD_FILE_PATH", configuration.RunRecordFilePath, filepath.Join(stateDirectory, "runs.jsonl"))
	configuration.CrontabBackupDirectory = pathUnlessConfigured(
		"CRONTAB_BACKUP_DIRECTORY", configuration.CrontabBackupDirectory, filepath.Join(stateDirectory, "backups"))

	configuration.DesktopLockFilePath = filepath.Join(stateDirectory, "desktop.lock")

	return configuration
}

// desktopStateDirectory 是狀態的落腳處。取不到家目錄時退回當前目錄下的 .state，
// 而不是崩掉 —— 沒有家目錄的環境極少見，但退到一個看得見的地方比整個起不來好。
func desktopStateDirectory() string {
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".state", desktopStateDirectoryName)
	}

	return filepath.Join(homeDirectory, ".local", "state", desktopStateDirectoryName)
}

// pathUnlessConfigured 只在使用者沒有明確設定時才替換掉那個容器預設值。
func pathUnlessConfigured(variableName string, currentValue string, desktopValue string) string {
	if strings.TrimSpace(os.Getenv(variableName)) != "" {
		return currentValue
	}

	return desktopValue
}
