package vo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

func TestParseCommandRedirectFindsTheRedirectTarget(t *testing.T) {
	testCases := []struct {
		name                  string
		command               string
		expectedBareCommand   string
		expectedTargetPath    string
		expectedAppends       bool
		expectedIncludesError bool
		expectedRawFragment   string
	}{
		{
			name:                  "append with stderr merged",
			command:               "/bin/backup.sh >> /var/log/backup.log 2>&1",
			expectedBareCommand:   "/bin/backup.sh",
			expectedTargetPath:    "/var/log/backup.log",
			expectedAppends:       true,
			expectedIncludesError: true,
			expectedRawFragment:   " >> /var/log/backup.log 2>&1",
		},
		{
			name:                  "truncating redirect without stderr",
			command:               "/bin/backup.sh > /var/log/backup.log",
			expectedBareCommand:   "/bin/backup.sh",
			expectedTargetPath:    "/var/log/backup.log",
			expectedAppends:       false,
			expectedIncludesError: false,
			expectedRawFragment:   " > /var/log/backup.log",
		},
		{
			name:                  "ampersand append redirects both streams",
			command:               "/bin/backup.sh &>> /var/log/backup.log",
			expectedBareCommand:   "/bin/backup.sh",
			expectedTargetPath:    "/var/log/backup.log",
			expectedAppends:       true,
			expectedIncludesError: true,
			expectedRawFragment:   " &>> /var/log/backup.log",
		},
		{
			name:                  "ampersand truncate redirects both streams",
			command:               "/bin/backup.sh &> /var/log/backup.log",
			expectedBareCommand:   "/bin/backup.sh",
			expectedTargetPath:    "/var/log/backup.log",
			expectedAppends:       false,
			expectedIncludesError: true,
			expectedRawFragment:   " &> /var/log/backup.log",
		},
		{
			name:                  "discarded output still parses to a target",
			command:               "/bin/backup.sh > /dev/null 2>&1",
			expectedBareCommand:   "/bin/backup.sh",
			expectedTargetPath:    "/dev/null",
			expectedAppends:       false,
			expectedIncludesError: true,
			expectedRawFragment:   " > /dev/null 2>&1",
		},
		{
			name:                  "stderr merge written before the file redirect",
			command:               "/bin/x 2>&1 >> /var/log/x.log",
			expectedBareCommand:   "/bin/x",
			expectedTargetPath:    "/var/log/x.log",
			expectedAppends:       true,
			expectedIncludesError: true,
			expectedRawFragment:   " 2>&1 >> /var/log/x.log",
		},
		{
			name:                  "stderr only redirect",
			command:               "/bin/x 2>> /var/log/x.err",
			expectedBareCommand:   "/bin/x",
			expectedTargetPath:    "/var/log/x.err",
			expectedAppends:       true,
			expectedIncludesError: true,
			expectedRawFragment:   " 2>> /var/log/x.err",
		},
		{
			name:                  "explicit stdout file descriptor",
			command:               "/bin/x 1>> /var/log/x.log",
			expectedBareCommand:   "/bin/x",
			expectedTargetPath:    "/var/log/x.log",
			expectedAppends:       true,
			expectedIncludesError: false,
			expectedRawFragment:   " 1>> /var/log/x.log",
		},
		{
			name:                  "no whitespace between operator and path",
			command:               "/bin/x >>/var/log/x.log",
			expectedBareCommand:   "/bin/x",
			expectedTargetPath:    "/var/log/x.log",
			expectedAppends:       true,
			expectedIncludesError: false,
			expectedRawFragment:   " >>/var/log/x.log",
		},
		{
			name:                  "command with arguments before the redirect",
			command:               "/usr/bin/rsync -av /src /dst >> /var/log/sync.log 2>&1",
			expectedBareCommand:   "/usr/bin/rsync -av /src /dst",
			expectedTargetPath:    "/var/log/sync.log",
			expectedAppends:       true,
			expectedIncludesError: true,
			expectedRawFragment:   " >> /var/log/sync.log 2>&1",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			bareCommand, redirect := vo.ParseCommandRedirect(testCase.command)

			require.NotNil(t, redirect)
			assert.Equal(t, testCase.expectedBareCommand, bareCommand)
			assert.Equal(t, testCase.expectedTargetPath, redirect.TargetFilePath())
			assert.Equal(t, testCase.expectedAppends, redirect.Appends())
			assert.Equal(t, testCase.expectedIncludesError, redirect.IncludesStandardError())
			assert.Equal(t, testCase.expectedRawFragment, redirect.RawFragment())
		})
	}
}

func TestParseCommandRedirectReportsNoRedirect(t *testing.T) {
	testCases := []struct {
		name    string
		command string
	}{
		{name: "plain command", command: "/bin/backup.sh"},
		{name: "command with arguments", command: "/usr/bin/rsync -av /src /dst"},
		{name: "greater than inside single quotes", command: "echo 'a > b'"},
		{name: "greater than inside double quotes", command: `echo "a > b"`},
		{name: "backgrounded command", command: "/bin/x &"},
		{name: "pipe without redirect", command: "/bin/x | grep foo"},
		{name: "digit in a filename is not a descriptor", command: "/bin/report2 --daily"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			bareCommand, redirect := vo.ParseCommandRedirect(testCase.command)

			assert.Nil(t, redirect)
			assert.Equal(t, testCase.command, bareCommand, "the command must come back untouched")
		})
	}
}

func TestCommandRedirectRecognisesDiscardedOutput(t *testing.T) {
	_, discardingRedirect := vo.ParseCommandRedirect("/bin/x > /dev/null 2>&1")
	require.NotNil(t, discardingRedirect)
	assert.True(t, discardingRedirect.DiscardsOutput())

	_, fileRedirect := vo.ParseCommandRedirect("/bin/x >> /var/log/x.log")
	require.NotNil(t, fileRedirect)
	assert.False(t, fileRedirect.DiscardsOutput())
}

func TestParseCommandRedirectWithoutATargetFileIsNotARedirect(t *testing.T) {
	// 只把 stderr 併進 stdout、沒有導向任何檔案 —— 輸出仍然是 cron 在管，我們
	// 拿不到，所以這不算「有 log 可讀」。
	bareCommand, redirect := vo.ParseCommandRedirect("/bin/x 2>&1")

	assert.Nil(t, redirect)
	assert.Equal(t, "/bin/x 2>&1", bareCommand)
}

func TestCommandRedirectRawFragmentAllowsExactRestoration(t *testing.T) {
	command := "/bin/backup.sh   >>  /var/log/backup.log 2>&1"

	bareCommand, redirect := vo.ParseCommandRedirect(command)
	require.NotNil(t, redirect)

	assert.Equal(t, command, bareCommand+redirect.RawFragment(),
		"bare command plus raw fragment must reproduce the original command exactly")
}
