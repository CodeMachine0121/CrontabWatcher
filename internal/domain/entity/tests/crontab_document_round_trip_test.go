package entity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

// 無損 round-trip 是本專案最重要的性質：寫壞使用者的 crontab 就等於弄壞他的
// 自動化。以下測試把它釘死。
func TestParseCrontabDocumentRoundTripsSyntheticContent(t *testing.T) {
	testCases := []struct {
		name    string
		content string
	}{
		{name: "empty file", content: ""},
		{name: "single newline", content: "\n"},
		{name: "several blank lines", content: "\n\n\n"},
		{name: "comments only", content: "# one\n# two\n"},
		{name: "with trailing newline", content: "0 3 * * * /bin/x\n"},
		{name: "without trailing newline", content: "0 3 * * * /bin/x"},
		{name: "windows line endings", content: "# c\r\n0 3 * * * /bin/x\r\n"},
		{name: "mixed line endings", content: "# c\r\n\n0 3 * * * /bin/x\r\n30 4 * * * /bin/y\n"},
		{name: "trailing whitespace preserved", content: "0 3 * * * /bin/x   \n"},
		{name: "leading whitespace preserved", content: "   0 3 * * * /bin/x\n"},
		{name: "internal alignment preserved", content: "0    3   *   *   *    /bin/x\n"},
		{name: "environment and blank lines", content: "SHELL=/bin/sh\n\nPATH=/usr/bin\n\n0 3 * * * /bin/x\n"},
		{name: "non-ascii comment", content: "# 每天凌晨三點跑備份\n0 3 * * * /bin/x\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			document := ParseCrontabDocument(testCase.content)

			assert.Equal(t, testCase.content, document.Render(),
				"Render must reproduce the input byte for byte")
		})
	}
}

func TestParseCrontabDocumentRoundTripsRealFixtures(t *testing.T) {
	fixtureNames := []string{
		"realistic_crontab.txt",
		"managed_crontab.txt",
		"crlf_crontab.txt",
	}

	for _, fixtureName := range fixtureNames {
		t.Run(fixtureName, func(t *testing.T) {
			contentBytes, err := os.ReadFile(filepath.Join("testdata", fixtureName))
			require.NoError(t, err)
			content := string(contentBytes)

			document := ParseCrontabDocument(content)

			assert.Equal(t, content, document.Render())
		})
	}
}

func TestParseCrontabDocumentNeverFails(t *testing.T) {
	// 這是別人的檔案。看不懂某一行不構成拒絕整份檔案的理由 —— 無法辨識的行
	// 一律歸為註解並原樣保留。
	hostileContent := "\x00\x01binary junk\nnot a cron line at all\n= = =\n0 3 * * * /bin/x\n"

	document := ParseCrontabDocument(hostileContent)

	assert.Equal(t, hostileContent, document.Render())
	assert.Len(t, document.Jobs(), 1, "the one valid entry is still found")
}

func TestCrontabDocumentLineCount(t *testing.T) {
	testCases := []struct {
		name              string
		content           string
		expectedLineCount int
	}{
		{name: "empty file has no lines", content: "", expectedLineCount: 0},
		{name: "single newline is one blank line", content: "\n", expectedLineCount: 1},
		{name: "two terminated lines", content: "a\nb\n", expectedLineCount: 2},
		{name: "unterminated last line still counts", content: "a\nb", expectedLineCount: 2},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			document := ParseCrontabDocument(testCase.content)

			assert.Len(t, document.Lines(), testCase.expectedLineCount)
		})
	}
}
