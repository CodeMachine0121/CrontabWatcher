package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 雙擊一個 app 時命令列上什麼都沒有。若那時預設成 serve，使用者會看到「雙擊了
// 但什麼都沒發生」——server 起來了，只是沒有任何畫面。
func TestResolveCommand(t *testing.T) {
	const bundledPath = "/Applications/CrontabWatcher.app/Contents/MacOS/cronwatch"
	const terminalPath = "/Users/james/workspace/CrontabWatcher/bin/cronwatch"

	testCases := []struct {
		name            string
		arguments       []string
		executablePath  string
		expectedCommand string
	}{
		{
			name:            "double clicked in the Applications folder",
			arguments:       []string{},
			executablePath:  bundledPath,
			expectedCommand: "desktop",
		},
		{
			name:            "launched by Finder with a process serial number",
			arguments:       []string{"-psn_0_774516"},
			executablePath:  bundledPath,
			expectedCommand: "desktop",
		},
		{
			name:            "run from a terminal with no subcommand",
			arguments:       []string{},
			executablePath:  terminalPath,
			expectedCommand: "serve",
		},
		{
			name:            "an explicit subcommand always wins, bundled or not",
			arguments:       []string{"run", "--job=job-1"},
			executablePath:  bundledPath,
			expectedCommand: "run",
		},
		{
			name:            "the window child process started from inside the bundle",
			arguments:       []string{"window", "--url=http://127.0.0.1:1/"},
			executablePath:  bundledPath,
			expectedCommand: "window",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expectedCommand,
				resolveCommand(testCase.arguments, testCase.executablePath))
		})
	}
}

func TestCommandArguments(t *testing.T) {
	assert.Equal(t, []string{"--job=job-1", "--", "/backup.sh"},
		commandArguments([]string{"run", "--job=job-1", "--", "/backup.sh"}))

	assert.Equal(t, []string{"--url=http://127.0.0.1:1/"},
		commandArguments([]string{"-psn_0_774516", "window", "--url=http://127.0.0.1:1/"}))

	assert.Empty(t, commandArguments([]string{"serve"}))
	assert.Empty(t, commandArguments([]string{}))
}
