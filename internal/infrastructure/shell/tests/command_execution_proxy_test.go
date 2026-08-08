package shell_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/shell"
)

const shellPath = "/bin/sh"

func newCommandExecutionProxy() *shell.CommandExecutionProxy {
	return shell.NewCommandExecutionProxy(shellPath)
}

func TestExecuteReturnsOutputAndSuccessfulExitCode(t *testing.T) {
	proxy := newCommandExecutionProxy()

	result, err := proxy.Execute(context.Background(), "echo hello", 0)

	require.NoError(t, err)
	assert.Equal(t, "hello\n", result.Output())
	assert.Equal(t, 0, result.ExitCode())
	assert.False(t, result.TimedOut())
}

func TestExecuteReportsANonZeroExitCode(t *testing.T) {
	proxy := newCommandExecutionProxy()

	result, err := proxy.Execute(context.Background(), "exit 3", 0)

	require.NoError(t, err, "a failing command is the job's problem, not an execution failure")
	assert.Equal(t, 3, result.ExitCode())
	assert.False(t, result.TimedOut())
}

func TestExecuteMergesStandardErrorIntoTheOutput(t *testing.T) {
	// 使用者要判斷 job 出了什麼事時，兩條流分開看毫無幫助。
	proxy := newCommandExecutionProxy()

	result, err := proxy.Execute(context.Background(), "echo to-stdout; echo to-stderr >&2", 0)

	require.NoError(t, err)
	assert.Contains(t, result.Output(), "to-stdout")
	assert.Contains(t, result.Output(), "to-stderr")
}

func TestExecuteReportsCommandNotFoundAsExitCode127(t *testing.T) {
	proxy := newCommandExecutionProxy()

	result, err := proxy.Execute(context.Background(), "this-command-does-not-exist-anywhere", 0)

	require.NoError(t, err)
	assert.Equal(t, 127, result.ExitCode())
	assert.NotEmpty(t, result.Output(), "the shell's complaint is part of what the user needs to see")
}

func TestExecuteKillsACommandThatExceedsItsTimeout(t *testing.T) {
	proxy := newCommandExecutionProxy()

	startedAt := time.Now()
	result, err := proxy.Execute(context.Background(), "sleep 30", 200*time.Millisecond)
	elapsed := time.Since(startedAt)

	require.NoError(t, err)
	assert.True(t, result.TimedOut())
	assert.Equal(t, -1, result.ExitCode())
	assert.Less(t, elapsed, 5*time.Second, "the command must be killed promptly, not waited out")
}

func TestExecuteWithoutATimeoutLetsTheCommandFinish(t *testing.T) {
	// 排程觸發的執行該跑多久就跑多久 —— 那是使用者自己排的 job。
	proxy := newCommandExecutionProxy()

	result, err := proxy.Execute(context.Background(), "sleep 0.3; echo finished", 0)

	require.NoError(t, err)
	assert.Equal(t, "finished\n", result.Output())
	assert.False(t, result.TimedOut())
}

func TestExecuteKillsTheWholeProcessGroupOnTimeout(t *testing.T) {
	// 只殺主程序會留下孤兒。這裡讓子孫程序在被殺之後才想寫檔案，若它活下來
	// 就會留下痕跡。
	proxy := newCommandExecutionProxy()
	markerFilePath := filepath.Join(t.TempDir(), "orphan-survived")

	command := fmt.Sprintf("( sleep 1; touch %s ) & wait", markerFilePath)
	result, err := proxy.Execute(context.Background(), command, 200*time.Millisecond)

	require.NoError(t, err)
	require.True(t, result.TimedOut())

	time.Sleep(2 * time.Second)

	_, statErr := os.Stat(markerFilePath)
	assert.True(t, os.IsNotExist(statErr),
		"a descendant process outlived the kill, so the timeout leaked an orphan")
}

func TestExecuteHandlesLargeOutputWithoutDeadlocking(t *testing.T) {
	// 若實作先等程序結束才讀 pipe，輸出填滿 pipe buffer 就會互相等死。
	proxy := newCommandExecutionProxy()

	result, err := proxy.Execute(context.Background(), "yes abcdefghijklmnopqrstuvwxyz | head -n 40000", 30*time.Second)

	require.NoError(t, err)
	assert.Equal(t, 40000, strings.Count(result.Output(), "\n"))
	assert.Equal(t, 0, result.ExitCode())
}

func TestExecuteDistinguishesAnExternalCancelFromATimeout(t *testing.T) {
	proxy := newCommandExecutionProxy()

	cancellableContext, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	startedAt := time.Now()
	result, err := proxy.Execute(cancellableContext, "sleep 30", 30*time.Second)
	elapsed := time.Since(startedAt)

	require.Error(t, err, "an external cancel means we did not carry the execution through")
	assert.False(t, result.TimedOut(), "it was cancelled, not timed out; conflating them would misreport the run")
	assert.Less(t, elapsed, 5*time.Second)
}

func TestExecuteFailsWhenTheShellItselfIsMissing(t *testing.T) {
	proxy := shell.NewCommandExecutionProxy("/nonexistent/shell")

	_, err := proxy.Execute(context.Background(), "echo hello", 0)

	assert.Error(t, err, "being unable to start anything at all is genuinely our failure")
}

func TestExecuteRunsTheCommandThroughTheShellSoRedirectionWorks(t *testing.T) {
	proxy := newCommandExecutionProxy()
	outputFilePath := filepath.Join(t.TempDir(), "written-by-shell")

	result, err := proxy.Execute(context.Background(), fmt.Sprintf("echo via-shell > %s", outputFilePath), 0)

	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode())

	contentBytes, err := os.ReadFile(outputFilePath)
	require.NoError(t, err)
	assert.Equal(t, "via-shell\n", string(contentBytes))
}
