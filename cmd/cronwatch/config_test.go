package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadServerConfigurationUsesDefaultsWhenNothingIsSet(t *testing.T) {
	configuration := loadServerConfiguration()

	assert.Equal(t, defaultServerAddress, configuration.ServerAddress,
		"the default binds loopback only; this service can execute arbitrary commands")
	assert.Equal(t, defaultCrontabFilePath, configuration.CrontabFilePath)
	assert.Equal(t, defaultRunLogDirectory, configuration.RunLogDirectory)
	assert.Equal(t, defaultRunRecordFilePath, configuration.RunRecordFilePath)
	assert.Equal(t, defaultCrontabBackupDirectory, configuration.CrontabBackupDirectory)
	assert.Equal(t, defaultShellPath, configuration.ShellPath)
	assert.Equal(t, defaultLogTailLines, configuration.LogTailLines)
	assert.Equal(t, defaultRunRecordRetentionCount, configuration.RunRecordRetentionCount)
	assert.Equal(t, defaultManualTriggerTimeout, configuration.ManualTriggerTimeout)
	assert.True(t, configuration.CrontabWriteEnabled)
	assert.True(t, configuration.ManualTriggerEnabled)
	assert.Equal(t, defaultTimeZoneName, configuration.Location.String())
}

func TestLoadServerConfigurationReadsOverrides(t *testing.T) {
	t.Setenv("SERVER_ADDRESS", "0.0.0.0:9000")
	t.Setenv("CRONTAB_FILE_PATH", "/custom/crontab")
	t.Setenv("RUN_LOG_DIRECTORY", "/custom/logs")
	t.Setenv("SHELL_PATH", "/bin/bash")
	t.Setenv("LOG_TAIL_LINES", "42")
	t.Setenv("RUN_RECORD_RETENTION_COUNT", "7")
	t.Setenv("MANUAL_TRIGGER_TIMEOUT_SECONDS", "60")
	t.Setenv("TZ", "UTC")

	configuration := loadServerConfiguration()

	assert.Equal(t, "0.0.0.0:9000", configuration.ServerAddress)
	assert.Equal(t, "/custom/crontab", configuration.CrontabFilePath)
	assert.Equal(t, "/custom/logs", configuration.RunLogDirectory)
	assert.Equal(t, "/bin/bash", configuration.ShellPath)
	assert.Equal(t, 42, configuration.LogTailLines)
	assert.Equal(t, 7, configuration.RunRecordRetentionCount)
	assert.Equal(t, time.Minute, configuration.ManualTriggerTimeout)
	assert.Equal(t, time.UTC, configuration.Location)
}

func TestBoolWithDefaultOnlyTreatsExplicitNegativesAsOff(t *testing.T) {
	testCases := []struct {
		value    string
		expected bool
	}{
		{value: "false", expected: false},
		{value: "FALSE", expected: false},
		{value: "0", expected: false},
		{value: "no", expected: false},
		{value: "off", expected: false},
		{value: "true", expected: true},
		{value: "1", expected: true},
		{value: "yes", expected: true},
		// 打錯的值視為開啟：這兩個開關預設都是開的，把看不懂的值當成「關」會讓
		// 使用者以為功能壞了，而不是以為自己拼錯字。
		{value: "ture", expected: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.value, func(t *testing.T) {
			t.Setenv("CRONTAB_WRITE_ENABLED", testCase.value)

			assert.Equal(t, testCase.expected, loadServerConfiguration().CrontabWriteEnabled)
		})
	}
}

func TestBoolWithDefaultFallsBackWhenUnset(t *testing.T) {
	t.Setenv("MANUAL_TRIGGER_ENABLED", "")

	assert.True(t, loadServerConfiguration().ManualTriggerEnabled)
}

func TestParseIntWithDefaultReportsUnparseableValues(t *testing.T) {
	t.Setenv("LOG_TAIL_LINES", "many")

	value, warning := parseIntWithDefault("LOG_TAIL_LINES", defaultLogTailLines)

	assert.Equal(t, defaultLogTailLines, value)
	assert.Contains(t, warning, "LOG_TAIL_LINES")
	assert.Contains(t, warning, "many")
}

func TestLoadServerConfigurationRaisesNonsensicalNumbers(t *testing.T) {
	// 靜靜地照著跑一個 0 行的 tail 只會讓人以為 log 是空的。
	t.Setenv("LOG_TAIL_LINES", "0")
	t.Setenv("MANUAL_TRIGGER_TIMEOUT_SECONDS", "0")

	configuration := loadServerConfiguration()

	assert.Equal(t, minimumLogTailLines, configuration.LogTailLines)
	assert.Equal(t, minimumManualTriggerTimeout, configuration.ManualTriggerTimeout)
	require.NotEmpty(t, configuration.Warnings, "silently fixing a mistyped value leaves the user confused later")
}

func TestLoadServerConfigurationWarnsAboutAnUnusableTimeZone(t *testing.T) {
	// 時區錯了只讓「下次執行」顯示錯，服務其餘部分仍然有用，所以不崩掉 ——
	// 但一定要說出來。
	t.Setenv("TZ", "Mars/Olympus_Mons")

	configuration := loadServerConfiguration()

	assert.Equal(t, time.UTC, configuration.Location)
	require.NotEmpty(t, configuration.Warnings)
	assert.Contains(t, joinWarnings(configuration.Warnings), "Mars/Olympus_Mons")
	assert.Contains(t, joinWarnings(configuration.Warnings), "UTC")
}

func TestLoadServerConfigurationHonoursAnExplicitWrapperBinaryPath(t *testing.T) {
	t.Setenv("WRAPPER_BINARY_PATH", "/opt/cronwatch/bin/cronwatch")

	configuration := loadServerConfiguration()

	assert.Equal(t, "/opt/cronwatch/bin/cronwatch", configuration.WrapperBinaryPath)
}

func TestLoadServerConfigurationWarnsWhenRunningFromATemporaryPath(t *testing.T) {
	// `go test` 與 `go run` 的執行檔都在暫存目錄下。把那個路徑寫進 crontab，
	// 條目會在檔案被清掉之後失效 —— 這個警告就是為了在動手改 crontab 之前先提醒。
	t.Setenv("WRAPPER_BINARY_PATH", "")

	configuration := loadServerConfiguration()

	require.NotEmpty(t, configuration.WrapperBinaryPath)
	if isTemporaryPath(configuration.WrapperBinaryPath) {
		assert.Contains(t, joinWarnings(configuration.Warnings), "WRAPPER_BINARY_PATH")
	}
}

func TestRunRecordRetentionCountOfZeroDisablesTrimming(t *testing.T) {
	t.Setenv("RUN_RECORD_RETENTION_COUNT", "-5")

	configuration := loadServerConfiguration()

	assert.Zero(t, configuration.RunRecordRetentionCount)
	assert.Contains(t, joinWarnings(configuration.Warnings), "RUN_RECORD_RETENTION_COUNT")
}

func joinWarnings(warnings []string) string {
	combined := ""
	for _, warning := range warnings {
		combined += warning + "\n"
	}

	return combined
}

func TestLoadServerConfigurationDefaultsToTheFileCrontabSource(t *testing.T) {
	configuration := loadServerConfiguration()

	assert.Equal(t, CrontabSourceFile, configuration.CrontabSource,
		"the default is the container mode, the one that does not touch a real user crontab")
	assert.Equal(t, defaultCrontabCommandPath, configuration.CrontabCommandPath)
	assert.False(t, configuration.UsesUserCrontab())
	assert.Equal(t, defaultCrontabFilePath, configuration.CrontabSourceDescription())
}

func TestLoadServerConfigurationReadsTheCommandCrontabSource(t *testing.T) {
	t.Setenv("CRONTAB_SOURCE", "crontabCommand")
	t.Setenv("CRONTAB_COMMAND_PATH", "/usr/bin/crontab")

	configuration := loadServerConfiguration()

	assert.Equal(t, CrontabSourceCommand, configuration.CrontabSource)
	assert.True(t, configuration.UsesUserCrontab())
	assert.Equal(t, "/usr/bin/crontab -l", configuration.CrontabSourceDescription(),
		"the UI has to say where the crontab actually came from")
}

func TestLoadServerConfigurationFallsBackOnAnUnrecognisedCrontabSource(t *testing.T) {
	// 退回 file 而不是 command：那是不會擅自去動使用者真正 crontab 的那一個。
	t.Setenv("CRONTAB_SOURCE", "hostt")

	configuration := loadServerConfiguration()

	assert.Equal(t, CrontabSourceFile, configuration.CrontabSource)
	assert.Contains(t, joinWarnings(configuration.Warnings), "CRONTAB_SOURCE")
	assert.Contains(t, joinWarnings(configuration.Warnings), "hostt")
}
