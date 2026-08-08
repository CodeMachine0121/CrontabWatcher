package entity_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

var runStartedAt = time.Date(2026, 8, 8, 14, 3, 1, 0, taipeiLocation)

func TestNewJobRunStartsRunning(t *testing.T) {
	run := NewJobRun("run-1", "job-1", TriggerSourceManual, runStartedAt)

	assert.Equal(t, "run-1", run.RunID())
	assert.Equal(t, "job-1", run.JobID())
	assert.Equal(t, TriggerSourceManual, run.TriggerSource())
	assert.Equal(t, RunStatusRunning, run.RunStatus())
	assert.Equal(t, runStartedAt, run.StartedAt())
	assert.False(t, run.IsFinished())
	assert.False(t, run.Succeeded())

	_, exitCodeKnown := run.ExitCode()
	assert.False(t, exitCodeKnown, "an unfinished run has no exit code")

	_, durationKnown := run.Duration()
	assert.False(t, durationKnown)
}

func TestJobRunFinish(t *testing.T) {
	testCases := []struct {
		name              string
		exitCode          int
		timedOut          bool
		expectedRunStatus RunStatus
		expectedSucceeded bool
	}{
		{name: "clean exit", exitCode: 0, expectedRunStatus: RunStatusSucceeded, expectedSucceeded: true},
		{name: "non zero exit", exitCode: 3, expectedRunStatus: RunStatusFailed},
		{name: "command not found", exitCode: 127, expectedRunStatus: RunStatusFailed},
		{name: "killed by timeout", exitCode: -1, timedOut: true, expectedRunStatus: RunStatusTimedOut},
		{name: "timeout wins over a zero exit code", exitCode: 0, timedOut: true, expectedRunStatus: RunStatusTimedOut},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			run := NewJobRun("run-1", "job-1", TriggerSourceSchedule, runStartedAt)
			finishedAt := runStartedAt.Add(1204 * time.Millisecond)

			run.Finish(finishedAt, testCase.exitCode, testCase.timedOut, "output")

			assert.Equal(t, testCase.expectedRunStatus, run.RunStatus())
			assert.Equal(t, testCase.expectedSucceeded, run.Succeeded())
			assert.True(t, run.IsFinished())

			exitCode, exitCodeKnown := run.ExitCode()
			require.True(t, exitCodeKnown)
			assert.Equal(t, testCase.exitCode, exitCode)

			duration, durationKnown := run.Duration()
			require.True(t, durationKnown)
			assert.Equal(t, 1204*time.Millisecond, duration)

			assert.Equal(t, "output", run.OutputExcerpt())
			assert.False(t, run.OutputTruncated())
		})
	}
}

func TestJobRunFinishTruncatesLongOutputKeepingTheTail(t *testing.T) {
	// 錯誤訊息通常在最後，所以截斷要保留尾端而不是開頭。
	longOutput := strings.Repeat("a", 9*1024) + "THE ACTUAL ERROR"

	run := NewJobRun("run-1", "job-1", TriggerSourceManual, runStartedAt)
	run.Finish(runStartedAt.Add(time.Second), 1, false, longOutput)

	assert.True(t, run.OutputTruncated())
	assert.LessOrEqual(t, len(run.OutputExcerpt()), JobRunOutputExcerptMaxBytes)
	assert.True(t, strings.HasSuffix(run.OutputExcerpt(), "THE ACTUAL ERROR"),
		"the tail carries the error, so the tail is what we keep")
}

func TestJobRunFinishTruncatesOnAValidUtf8Boundary(t *testing.T) {
	// 「備」是三個 byte。若在 byte 邊界硬切，會產生半個字元、在網頁上變成亂碼。
	longOutput := strings.Repeat("備", 4*1024)

	run := NewJobRun("run-1", "job-1", TriggerSourceManual, runStartedAt)
	run.Finish(runStartedAt.Add(time.Second), 0, false, longOutput)

	assert.True(t, run.OutputTruncated())
	assert.True(t, utf8.ValidString(run.OutputExcerpt()), "the excerpt must stay valid UTF-8")
}

func TestJobRunFinishKeepsShortOutputIntact(t *testing.T) {
	run := NewJobRun("run-1", "job-1", TriggerSourceManual, runStartedAt)
	run.Finish(runStartedAt.Add(time.Second), 0, false, "hello\nworld")

	assert.Equal(t, "hello\nworld", run.OutputExcerpt())
	assert.False(t, run.OutputTruncated())
}

func TestJobRunMarkInterrupted(t *testing.T) {
	// server 被砍掉時，執行中的紀錄會留在檔案裡。啟動時要把它們標成無法判定，
	// 而不是留著假裝還在跑。
	run := NewJobRun("run-1", "job-1", TriggerSourceManual, runStartedAt)

	run.MarkInterrupted(runStartedAt.Add(time.Minute))

	assert.Equal(t, RunStatusUnknown, run.RunStatus())
	assert.True(t, run.IsFinished())
	assert.Contains(t, run.OutputExcerpt(), "interrupted by restart")

	_, exitCodeKnown := run.ExitCode()
	assert.False(t, exitCodeKnown, "an interrupted run's exit code is genuinely unknown")
}

func TestJobRunStatusNormalisation(t *testing.T) {
	testCases := []struct {
		value    string
		expected RunStatus
	}{
		{value: "running", expected: RunStatusRunning},
		{value: "succeeded", expected: RunStatusSucceeded},
		{value: "failed", expected: RunStatusFailed},
		{value: "timedOut", expected: RunStatusTimedOut},
		{value: "unknown", expected: RunStatusUnknown},
		{value: "", expected: RunStatusUnknown},
		{value: "garbage", expected: RunStatusUnknown},
		{value: "SUCCEEDED", expected: RunStatusUnknown},
	}

	for _, testCase := range testCases {
		t.Run(testCase.value, func(t *testing.T) {
			assert.Equal(t, testCase.expected, NewRunStatus(testCase.value))
		})
	}
}

func TestTriggerSourceNormalisation(t *testing.T) {
	assert.Equal(t, TriggerSourceManual, NewTriggerSource("manual"))
	assert.Equal(t, TriggerSourceSchedule, NewTriggerSource("schedule"))
	assert.Equal(t, TriggerSourceSchedule, NewTriggerSource("garbage"))
	assert.Equal(t, TriggerSourceSchedule, NewTriggerSource(""))
}

func TestRestoreJobRunRebuildsAPersistedRun(t *testing.T) {
	finishedAt := runStartedAt.Add(2 * time.Second)

	run := RestoreJobRun(
		"run-1", "job-1", "manual", "failed",
		runStartedAt, finishedAt, 3, true, "boom", true)

	assert.Equal(t, "run-1", run.RunID())
	assert.Equal(t, TriggerSourceManual, run.TriggerSource())
	assert.Equal(t, RunStatusFailed, run.RunStatus())
	assert.True(t, run.IsFinished())

	exitCode, exitCodeKnown := run.ExitCode()
	require.True(t, exitCodeKnown)
	assert.Equal(t, 3, exitCode)

	duration, durationKnown := run.Duration()
	require.True(t, durationKnown)
	assert.Equal(t, 2*time.Second, duration)

	assert.True(t, run.OutputTruncated())
}

func TestRestoreJobRunOfAnUnfinishedRun(t *testing.T) {
	run := RestoreJobRun(
		"run-1", "job-1", "schedule", "running",
		runStartedAt, time.Time{}, 0, false, "", false)

	assert.Equal(t, RunStatusRunning, run.RunStatus())
	assert.False(t, run.IsFinished())

	_, exitCodeKnown := run.ExitCode()
	assert.False(t, exitCodeKnown)
}

func TestJobRunLogHeaderAndFooter(t *testing.T) {
	run := NewJobRun("run-1", "job-1", TriggerSourceManual, runStartedAt)

	header := run.BuildLogHeader()
	assert.Contains(t, header, "run-1")
	assert.Contains(t, header, "manual")
	assert.Contains(t, header, runStartedAt.Format(time.RFC3339))
	assert.True(t, strings.HasSuffix(header, "\n"))

	run.Finish(runStartedAt.Add(1204*time.Millisecond), 0, false, "out")

	footer := run.BuildLogFooter()
	assert.Contains(t, footer, "exit=0")
	assert.Contains(t, footer, "1.204s")
	assert.True(t, strings.HasSuffix(footer, "\n"))
}
