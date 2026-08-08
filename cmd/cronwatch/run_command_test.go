package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
)

func TestParseRunArguments(t *testing.T) {
	testCases := []struct {
		name            string
		arguments       []string
		expectedJobID   string
		expectedCommand string
	}{
		{
			name:            "plain command",
			arguments:       []string{"--job=job-1", "--", "/usr/local/bin/backup.sh"},
			expectedJobID:   "job-1",
			expectedCommand: "/usr/local/bin/backup.sh",
		},
		{
			name:            "command with flags stays readable",
			arguments:       []string{"--job=job-1", "--", "/usr/bin/rsync", "-av", "/src", "/dst"},
			expectedJobID:   "job-1",
			expectedCommand: "/usr/bin/rsync -av /src /dst",
		},
		{
			// cron 把整行交給 shell，shell 在 exec 我們之前就吃掉了引號。用空白
			// 接回去會把 `sh -c 'echo hi'` 變成 `sh -c echo hi`，語意完全不同。
			name:            "quoting is restored, not lost",
			arguments:       []string{"--job=job-1", "--", "sh", "-c", "echo hi; exit 3"},
			expectedJobID:   "job-1",
			expectedCommand: `sh -c 'echo hi; exit 3'`,
		},
		{
			name:            "sql style argument",
			arguments:       []string{"--job=job-1", "--", "mysql", "-e", "SELECT count(*) FROM orders"},
			expectedJobID:   "job-1",
			expectedCommand: `mysql -e 'SELECT count(*) FROM orders'`,
		},
		{
			name:            "argument containing a single quote",
			arguments:       []string{"--job=job-1", "--", "echo", "it's fine"},
			expectedJobID:   "job-1",
			expectedCommand: `echo 'it'\''s fine'`,
		},
		{
			name:            "empty argument is preserved",
			arguments:       []string{"--job=job-1", "--", "/bin/x", ""},
			expectedJobID:   "job-1",
			expectedCommand: "/bin/x ''",
		},
		{
			name:            "leading dashes in the command are not read as our own flags",
			arguments:       []string{"--job=job-1", "--", "/bin/x", "--job=not-ours", "--help"},
			expectedJobID:   "job-1",
			expectedCommand: "/bin/x --job=not-ours --help",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			jobID, command, err := parseRunArguments(testCase.arguments)

			require.NoError(t, err)
			assert.Equal(t, testCase.expectedJobID, jobID)
			assert.Equal(t, testCase.expectedCommand, command)
		})
	}
}

func TestParseRunArgumentsRejectsBadUsage(t *testing.T) {
	testCases := []struct {
		name      string
		arguments []string
	}{
		{name: "no arguments at all", arguments: []string{}},
		{name: "missing job identifier", arguments: []string{"--", "/bin/x"}},
		{name: "missing separator", arguments: []string{"--job=job-1", "/bin/x"}},
		{name: "nothing after the separator", arguments: []string{"--job=job-1", "--"}},
		{name: "empty job identifier", arguments: []string{"--job=", "--", "/bin/x"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := parseRunArguments(testCase.arguments)

			assert.Error(t, err)
		})
	}
}

func TestShellQuoteLeavesSafeArgumentsAlone(t *testing.T) {
	// 已經安全的參數保持原樣，這樣 UI 與 log 上的指令仍然是人看得懂的形狀。
	for _, safeArgument := range []string{"/usr/local/bin/backup.sh", "-av", "--daily", "src/data.csv"} {
		assert.Equal(t, safeArgument, shellQuote(safeArgument))
	}
}

func TestExitCodeFromRun(t *testing.T) {
	exitCodeThree := 3
	killedExitCode := -1

	testCases := []struct {
		name         string
		runDto       dto.JobRunDto
		expectedCode int
	}{
		{
			name:         "clean exit is passed through",
			runDto:       dto.JobRunDto{ExitCode: new(int)},
			expectedCode: 0,
		},
		{
			name:         "failure code is passed through so cron sees the same result",
			runDto:       dto.JobRunDto{ExitCode: &exitCodeThree},
			expectedCode: 3,
		},
		{
			// 被 kill 的程序沒有真正的 exit code；137 是 shell 對 SIGKILL 的慣例。
			name:         "a killed process reports the conventional signal code",
			runDto:       dto.JobRunDto{ExitCode: &killedExitCode},
			expectedCode: 137,
		},
		{
			// 無從得知時宣稱成功是最糟的選擇。
			name:         "an unknown exit code is reported as a failure",
			runDto:       dto.JobRunDto{ExitCode: nil},
			expectedCode: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expectedCode, exitCodeFromRun(testCase.runDto))
		})
	}
}

func TestShellQuoteDoesNotBreakAnEnvironmentAssignmentPrefix(t *testing.T) {
	// `FOO=bar /bin/x` 是合法的 crontab 指令：前置的賦值只對這道指令生效。
	// 把 FOO=bar 引起來，shell 會改成去找一個叫 "FOO=bar" 的執行檔。
	_, command, err := parseRunArguments([]string{"--job=job-1", "--", "TZ=UTC", "/bin/report.sh"})

	require.NoError(t, err)
	assert.Equal(t, "TZ=UTC /bin/report.sh", command)
}
