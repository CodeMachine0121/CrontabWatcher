package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 預設值。全部都有預設值，因此 .env 可以整份省略。
const (
	defaultServerAddress           = "127.0.0.1:8080"
	defaultCrontabFilePath         = "/data/crontabs/root"
	defaultRunLogDirectory         = "/data/logs"
	defaultRunRecordFilePath       = "/data/runs.jsonl"
	defaultCrontabBackupDirectory  = "/data/backups"
	defaultShellPath               = "/bin/sh"
	defaultTimeZoneName            = "Asia/Taipei"
	defaultLogTailLines            = 200
	defaultRunRecordRetentionCount = 500
	defaultManualTriggerTimeout    = 900 * time.Second

	// minimumLogTailLines 與 minimumManualTriggerTimeout 是正規化的下界。
	// 設成 0 或負數多半是打錯，靜靜地照著跑只會產生難以理解的行為。
	minimumLogTailLines         = 1
	minimumManualTriggerTimeout = time.Second
)

// ServerConfiguration 是啟動時一次讀完的組態。
type ServerConfiguration struct {
	ServerAddress string

	CrontabFilePath        string
	RunLogDirectory        string
	RunRecordFilePath      string
	CrontabBackupDirectory string

	CrontabWriteEnabled  bool
	ManualTriggerEnabled bool
	ManualTriggerTimeout time.Duration

	ShellPath               string
	LogTailLines            int
	RunRecordRetentionCount int

	// WrapperBinaryPath 是寫進 crontab 條目的執行檔路徑。
	WrapperBinaryPath string

	// Location 是計算「下次執行時間」用的時區。cron 的排程語意綁在時區上。
	Location *time.Location

	// Warnings 收集組態正規化時發現的問題，由 main 印出來 —— 靜靜地修正使用者
	// 打錯的值，會讓他之後對著錯誤的行為百思不解。
	Warnings []string
}

// loadServerConfiguration 讀取並正規化環境變數。
func loadServerConfiguration() ServerConfiguration {
	warnings := make([]string, 0)

	logTailLines, warning := parseIntWithDefault("LOG_TAIL_LINES", defaultLogTailLines)
	warnings = appendWarning(warnings, warning)
	if logTailLines < minimumLogTailLines {
		warnings = append(warnings, fmt.Sprintf(
			"LOG_TAIL_LINES was %d, raised to %d", logTailLines, minimumLogTailLines))
		logTailLines = minimumLogTailLines
	}

	retentionCount, warning := parseIntWithDefault("RUN_RECORD_RETENTION_COUNT", defaultRunRecordRetentionCount)
	warnings = appendWarning(warnings, warning)
	if retentionCount < 1 {
		warnings = append(warnings, fmt.Sprintf(
			"RUN_RECORD_RETENTION_COUNT was %d, run history will not be trimmed", retentionCount))
		retentionCount = 0
	}

	timeoutSeconds, warning := parseIntWithDefault(
		"MANUAL_TRIGGER_TIMEOUT_SECONDS", int(defaultManualTriggerTimeout.Seconds()))
	warnings = appendWarning(warnings, warning)
	manualTriggerTimeout := time.Duration(timeoutSeconds) * time.Second
	if manualTriggerTimeout < minimumManualTriggerTimeout {
		warnings = append(warnings, fmt.Sprintf(
			"MANUAL_TRIGGER_TIMEOUT_SECONDS was %d, raised to %d", timeoutSeconds, int(minimumManualTriggerTimeout.Seconds())))
		manualTriggerTimeout = minimumManualTriggerTimeout
	}

	location, warning := loadLocationFromEnvironment()
	warnings = appendWarning(warnings, warning)

	wrapperBinaryPath, warning := resolveWrapperBinaryPath()
	warnings = appendWarning(warnings, warning)

	return ServerConfiguration{
		ServerAddress:           stringWithDefault("SERVER_ADDRESS", defaultServerAddress),
		CrontabFilePath:         stringWithDefault("CRONTAB_FILE_PATH", defaultCrontabFilePath),
		RunLogDirectory:         stringWithDefault("RUN_LOG_DIRECTORY", defaultRunLogDirectory),
		RunRecordFilePath:       stringWithDefault("RUN_RECORD_FILE_PATH", defaultRunRecordFilePath),
		CrontabBackupDirectory:  stringWithDefault("CRONTAB_BACKUP_DIRECTORY", defaultCrontabBackupDirectory),
		CrontabWriteEnabled:     boolWithDefault("CRONTAB_WRITE_ENABLED", true),
		ManualTriggerEnabled:    boolWithDefault("MANUAL_TRIGGER_ENABLED", true),
		ManualTriggerTimeout:    manualTriggerTimeout,
		ShellPath:               stringWithDefault("SHELL_PATH", defaultShellPath),
		LogTailLines:            logTailLines,
		RunRecordRetentionCount: retentionCount,
		WrapperBinaryPath:       wrapperBinaryPath,
		Location:                location,
		Warnings:                warnings,
	}
}

func stringWithDefault(variableName string, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(variableName)); value != "" {
		return value
	}

	return defaultValue
}

// boolWithDefault 只把明確的 "false"／"0"／"no" 當成關閉。
func boolWithDefault(variableName string, defaultValue bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(variableName)))
	switch value {
	case "":
		return defaultValue
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

// parseIntWithDefault 解析整數，無法解析時回預設值並附上警告。
func parseIntWithDefault(variableName string, defaultValue int) (int, string) {
	value := strings.TrimSpace(os.Getenv(variableName))
	if value == "" {
		return defaultValue, ""
	}

	parsedValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, fmt.Sprintf("%s is not a number (%q), using %d", variableName, value, defaultValue)
	}

	return parsedValue, ""
}

// loadLocationFromEnvironment 依 TZ 載入時區。
//
// 載不到就退回 UTC 並警告，而不是直接崩掉：時區錯了只會讓「下次執行時間」顯示錯，
// 服務其餘部分仍然有用。但一定要說出來，否則使用者會對著錯的時間困惑。
func loadLocationFromEnvironment() (*time.Location, string) {
	timeZoneName := stringWithDefault("TZ", defaultTimeZoneName)

	location, err := time.LoadLocation(timeZoneName)
	if err != nil {
		return time.UTC, fmt.Sprintf(
			"TZ %q could not be loaded (%v), falling back to UTC; next run times will be shown in UTC", timeZoneName, err)
	}

	return location, ""
}

// resolveWrapperBinaryPath 決定要寫進 crontab 的執行檔路徑。
//
// 預設用自己的執行檔絕對路徑。但 go run 產生的是暫存目錄下的檔案，寫進 crontab
// 之後那個路徑很快就不存在了 —— 所以必須能用環境變數覆寫，而且在偵測到暫存路徑
// 時主動警告。
func resolveWrapperBinaryPath() (string, string) {
	if configured := strings.TrimSpace(os.Getenv("WRAPPER_BINARY_PATH")); configured != "" {
		return configured, ""
	}

	executablePath, err := os.Executable()
	if err != nil {
		return "cronwatch", fmt.Sprintf(
			"could not determine our own executable path (%v); crontab entries will use the bare name "+
				"\"cronwatch\", which only works if it is on cron's PATH. Set WRAPPER_BINARY_PATH", err)
	}

	resolvedPath, err := filepath.Abs(executablePath)
	if err != nil {
		resolvedPath = executablePath
	}

	if isTemporaryPath(resolvedPath) {
		return resolvedPath, fmt.Sprintf(
			"this binary lives under a temporary directory (%s), which is what `go run` does. "+
				"Managed crontab entries written now will stop working once it is cleaned up. "+
				"Set WRAPPER_BINARY_PATH to the installed path", resolvedPath)
	}

	return resolvedPath, ""
}

func isTemporaryPath(path string) bool {
	temporaryPrefixes := []string{os.TempDir(), "/tmp", "/var/folders"}

	for _, prefix := range temporaryPrefixes {
		if prefix != "" && strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

func appendWarning(warnings []string, warning string) []string {
	if warning == "" {
		return warnings
	}

	return append(warnings, warning)
}
