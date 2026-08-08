package runlog_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/runlog"
)

func newJobLogRepository(t *testing.T) (*runlog.JobLogRepository, string) {
	t.Helper()

	return runlog.NewJobLogRepository(), filepath.Join(t.TempDir(), "logs", "job-1.log")
}

func writeLogFile(t *testing.T, filePath string, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o700))
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o600))
}

func TestTailReturnsTheLastLines(t *testing.T) {
	repository, logFilePath := newJobLogRepository(t)

	lines := make([]string, 0, 10)
	for lineNumber := 1; lineNumber <= 10; lineNumber++ {
		lines = append(lines, fmt.Sprintf("line %d", lineNumber))
	}
	writeLogFile(t, logFilePath, strings.Join(lines, "\n")+"\n")

	tail, err := repository.Tail(logFilePath, 5)

	require.NoError(t, err)
	assert.True(t, tail.Exists())
	assert.False(t, tail.Truncated())
	assert.Equal(t, "line 6\nline 7\nline 8\nline 9\nline 10\n", tail.Content())
	assert.Equal(t, 5, tail.LineCount())
}

func TestTailReturnsEverythingWhenAskedForMoreLinesThanExist(t *testing.T) {
	repository, logFilePath := newJobLogRepository(t)
	writeLogFile(t, logFilePath, "one\ntwo\nthree\n")

	tail, err := repository.Tail(logFilePath, 100)

	require.NoError(t, err)
	assert.Equal(t, "one\ntwo\nthree\n", tail.Content())
	assert.Equal(t, 3, tail.LineCount())
}

func TestTailOnAMissingFile(t *testing.T) {
	// job 還沒跑過。這是正常狀態，不是錯誤 —— 但必須與「跑過而沒有輸出」區分。
	repository, logFilePath := newJobLogRepository(t)

	tail, err := repository.Tail(logFilePath, 10)

	require.NoError(t, err)
	assert.False(t, tail.Exists())
	assert.Empty(t, tail.Content())
}

func TestTailOnAnEmptyFile(t *testing.T) {
	repository, logFilePath := newJobLogRepository(t)
	writeLogFile(t, logFilePath, "")

	tail, err := repository.Tail(logFilePath, 10)

	require.NoError(t, err)
	assert.True(t, tail.Exists(), "the file exists, it just has nothing in it")
	assert.Empty(t, tail.Content())
	assert.Zero(t, tail.LineCount())
}

func TestTailKeepsAnUnterminatedFinalLine(t *testing.T) {
	repository, logFilePath := newJobLogRepository(t)
	writeLogFile(t, logFilePath, "one\ntwo\nthree without newline")

	tail, err := repository.Tail(logFilePath, 2)

	require.NoError(t, err)
	assert.Equal(t, "two\nthree without newline", tail.Content())
}

func TestTailClampsANonPositiveLineCount(t *testing.T) {
	repository, logFilePath := newJobLogRepository(t)
	writeLogFile(t, logFilePath, "one\ntwo\n")

	for _, requestedLines := range []int{0, -1, -100} {
		tail, err := repository.Tail(logFilePath, requestedLines)

		require.NoError(t, err)
		assert.Equal(t, "two\n", tail.Content(), "a non-positive request is clamped to one line")
	}
}

func TestTailReadsOnlyTheEndOfALargeFile(t *testing.T) {
	// 這個測試守的是「不要整檔載入記憶體」。若實作改成讀整份檔案，時間會隨檔案
	// 大小成長，這裡就會失敗。
	repository, logFilePath := newJobLogRepository(t)

	var builder strings.Builder
	for lineNumber := 1; lineNumber <= 400_000; lineNumber++ {
		fmt.Fprintf(&builder, "line %d padding padding padding padding\n", lineNumber)
	}
	content := builder.String()
	require.Greater(t, len(content), 8*1024*1024, "the fixture must exceed the read cap by a wide margin")
	writeLogFile(t, logFilePath, content)

	startedAt := time.Now()
	tail, err := repository.Tail(logFilePath, 200)
	elapsed := time.Since(startedAt)

	require.NoError(t, err)
	assert.Equal(t, 200, tail.LineCount())
	assert.True(t, strings.HasSuffix(tail.Content(), "line 400000 padding padding padding padding\n"))
	assert.Less(t, elapsed, 500*time.Millisecond, "tailing must not scale with file size")
}

func TestTailMarksTruncationWhenTheReadCapIsHit(t *testing.T) {
	repository, logFilePath := newJobLogRepository(t)

	singleLine := strings.Repeat("x", 4096) + "\n"
	writeLogFile(t, logFilePath, strings.Repeat(singleLine, 1000)) // 約 4 MiB

	tail, err := repository.Tail(logFilePath, 1000)

	require.NoError(t, err)
	assert.True(t, tail.Truncated(), "the byte cap was reached before the requested line count")
	assert.LessOrEqual(t, len(tail.Content()), runlog.JobLogTailMaxBytes)
}

func TestTailHandlesASingleLineLongerThanTheChunkSize(t *testing.T) {
	repository, logFilePath := newJobLogRepository(t)
	writeLogFile(t, logFilePath, strings.Repeat("y", 200*1024)+"\n")

	tail, err := repository.Tail(logFilePath, 10)

	require.NoError(t, err)
	assert.True(t, tail.Exists())
	assert.NotEmpty(t, tail.Content(), "a line larger than one chunk must not come back empty")
}

func TestTailReplacesInvalidUtf8(t *testing.T) {
	// log 檔可能含二進位垃圾。網頁上顯示替代字元，總比讓 JSON 編碼整個失敗好。
	repository, logFilePath := newJobLogRepository(t)
	writeLogFile(t, logFilePath, "good line\n\xff\xfe broken bytes\n")

	tail, err := repository.Tail(logFilePath, 10)

	require.NoError(t, err)
	assert.True(t, utf8.ValidString(tail.Content()))
	assert.Contains(t, tail.Content(), "good line")
}

func TestTailDoesNotSplitAMultiByteCharacter(t *testing.T) {
	repository, logFilePath := newJobLogRepository(t)
	writeLogFile(t, logFilePath, strings.Repeat("備份完成\n", 1000))

	tail, err := repository.Tail(logFilePath, 3)

	require.NoError(t, err)
	assert.Equal(t, "備份完成\n備份完成\n備份完成\n", tail.Content())
}

func TestAppendCreatesTheFileAndItsDirectory(t *testing.T) {
	repository, logFilePath := newJobLogRepository(t)

	require.NoError(t, repository.Append(logFilePath, "first\n"))
	require.NoError(t, repository.Append(logFilePath, "second\n"))

	contentBytes, err := os.ReadFile(logFilePath)
	require.NoError(t, err)
	assert.Equal(t, "first\nsecond\n", string(contentBytes))
}

func TestAppendFailsOnAnUnwritableDirectory(t *testing.T) {
	repository, _ := newJobLogRepository(t)
	readOnlyDirectory := t.TempDir()
	require.NoError(t, os.Chmod(readOnlyDirectory, 0o500))
	t.Cleanup(func() { _ = os.Chmod(readOnlyDirectory, 0o700) })

	err := repository.Append(filepath.Join(readOnlyDirectory, "job.log"), "content")

	assert.Error(t, err)
}
