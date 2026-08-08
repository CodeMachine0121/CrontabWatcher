package runlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

const (
	// JobLogTailMaxBytes 是單次 tail 的讀取上限。log 檔可以長到幾百 MB，整檔
	// 載入會把記憶體吃光；1 MiB 已遠超任何人在網頁上會讀的量。
	JobLogTailMaxBytes = 1 << 20
	// jobLogTailChunkBytes 是每次往回讀的區塊大小。
	jobLogTailChunkBytes = 64 * 1024

	logFilePermission      = 0o600
	logDirectoryPermission = 0o700
)

// JobLogRepository 讀寫 job 的 log 檔。
type JobLogRepository struct{}

// NewJobLogRepository 建立 repository。它沒有狀態 —— 檔案路徑每次由呼叫端指定，
// 因為 foreign job 的 log 可能在系統任何地方。
func NewJobLogRepository() *JobLogRepository {
	return &JobLogRepository{}
}

// Tail 從檔尾往回讀最多 lines 行。
//
// 刻意不整檔載入：從檔尾以區塊往前掃，讀到足夠的換行或觸及上限就停。這讓回應
// 時間與檔案大小無關。
func (repository *JobLogRepository) Tail(filePath string, lines int) (vo.JobLogTail, error) {
	if lines < 1 {
		lines = 1
	}

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return vo.NewMissingJobLogTail(), nil
		}

		return vo.JobLogTail{}, fmt.Errorf("opening log file %s: %w", filePath, err)
	}
	defer file.Close()

	fileInformation, err := file.Stat()
	if err != nil {
		return vo.JobLogTail{}, fmt.Errorf("inspecting log file %s: %w", filePath, err)
	}

	fileSize := fileInformation.Size()
	if fileSize == 0 {
		return vo.NewJobLogTail("", true, false), nil
	}

	content, truncated, err := readTailBytes(file, fileSize, lines)
	if err != nil {
		return vo.JobLogTail{}, fmt.Errorf("reading log file %s: %w", filePath, err)
	}

	return vo.NewJobLogTail(sanitiseLogContent(content), true, truncated), nil
}

// Append 把內容附加到 log 檔尾端，必要時建立父目錄。
func (repository *JobLogRepository) Append(filePath string, content string) error {
	if err := os.MkdirAll(filepath.Dir(filePath), logDirectoryPermission); err != nil {
		return fmt.Errorf("creating log directory for %s: %w", filePath, err)
	}

	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, logFilePermission)
	if err != nil {
		return fmt.Errorf("opening log file %s: %w", filePath, err)
	}

	if _, err := file.WriteString(content); err != nil {
		file.Close()
		return fmt.Errorf("appending to log file %s: %w", filePath, err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("closing log file %s: %w", filePath, err)
	}

	return nil
}

// readTailBytes 從檔尾往前讀，直到湊齊 lines 行或觸及讀取上限。
func readTailBytes(file *os.File, fileSize int64, lines int) (string, bool, error) {
	collectedChunks := make([][]byte, 0)
	collectedBytes := 0
	newlineCount := 0
	cursor := fileSize
	truncated := false

	for cursor > 0 {
		chunkSize := int64(jobLogTailChunkBytes)
		if remainingBytes := int64(JobLogTailMaxBytes - collectedBytes); chunkSize > remainingBytes {
			chunkSize = remainingBytes
		}
		if chunkSize > cursor {
			chunkSize = cursor
		}
		if chunkSize <= 0 {
			truncated = true
			break
		}

		chunk := make([]byte, chunkSize)
		readAt := cursor - chunkSize
		if _, err := file.ReadAt(chunk, readAt); err != nil {
			return "", false, err
		}

		collectedChunks = append(collectedChunks, chunk)
		collectedBytes += len(chunk)
		cursor = readAt

		// 最後一個換行是行的結尾而非分隔，不計入，否則會少回一行。
		newlineCount = countNewlinesExcludingTrailing(collectedChunks)
		if newlineCount >= lines {
			break
		}

		if collectedBytes >= JobLogTailMaxBytes {
			truncated = cursor > 0
			break
		}
	}

	content := joinReversedChunks(collectedChunks)

	if newlineCount >= lines {
		content = keepLastLines(content, lines)
	}

	// 觸及上限時，第一行很可能是被切斷的殘片，丟掉它比顯示半行好。
	if truncated {
		if firstNewlineIndex := strings.IndexByte(content, '\n'); firstNewlineIndex >= 0 {
			content = content[firstNewlineIndex+1:]
		}
	}

	return content, truncated, nil
}

func countNewlinesExcludingTrailing(reversedChunks [][]byte) int {
	content := joinReversedChunks(reversedChunks)
	trimmedContent := strings.TrimSuffix(content, "\n")

	return strings.Count(trimmedContent, "\n")
}

func joinReversedChunks(reversedChunks [][]byte) string {
	var builder strings.Builder
	for index := len(reversedChunks) - 1; index >= 0; index-- {
		builder.Write(reversedChunks[index])
	}

	return builder.String()
}

// keepLastLines 只留下最後 lines 行。
func keepLastLines(content string, lines int) string {
	hadTrailingNewline := strings.HasSuffix(content, "\n")
	trimmedContent := strings.TrimSuffix(content, "\n")

	allLines := strings.Split(trimmedContent, "\n")
	if len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}

	result := strings.Join(allLines, "\n")
	if hadTrailingNewline {
		result += "\n"
	}

	return result
}

// sanitiseLogContent 把不合法的 UTF-8 換成替代字元。
//
// log 檔可能含二進位垃圾。在網頁上顯示替代字元，遠好過讓 JSON 編碼整個失敗、
// 使用者連一行都看不到。
func sanitiseLogContent(content string) string {
	if utf8.ValidString(content) {
		return content
	}

	return strings.ToValidUTF8(content, "�")
}
