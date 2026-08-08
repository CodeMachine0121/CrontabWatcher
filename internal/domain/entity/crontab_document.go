package entity

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

// CrontabDocument 是一整份 crontab 檔案。
//
// 內部只是一串保留了原始文字與行尾字元的 CrontabLine；Render() 就是把它們接回去。
// 所有修改操作都只替換／插入／刪除特定的行，因此「除了要改的那行以外一字不差」
// 是結構上的保證，而不是靠每個操作各自小心維護。
type CrontabDocument struct {
	lines []vo.CrontabLine
}

// NewCrontabDocument 由一組行建立文件。
func NewCrontabDocument(lines []vo.CrontabLine) *CrontabDocument {
	return &CrontabDocument{lines: lines}
}

// ParseCrontabDocument 解析 crontab 檔案內容。
//
// 這個函式永不失敗。這是別人的檔案，看不懂某一行不構成拒絕整份檔案的理由 ——
// 無法辨識的行一律歸為註解並原樣保留。
func ParseCrontabDocument(content string) *CrontabDocument {
	return NewCrontabDocument(splitCrontabLines(content))
}

// splitCrontabLines 切行，並讓每一行各自帶著它原本的行尾字元。
//
// 真實檔案會混用 LF 與 CRLF，最後一行也可能沒有行尾。把行尾存在行上，而不是
// 整份檔案共用一個設定，是無損還原的關鍵。
func splitCrontabLines(content string) []vo.CrontabLine {
	if content == "" {
		return nil
	}

	lines := make([]vo.CrontabLine, 0, strings.Count(content, "\n")+1)

	for cursor := 0; cursor < len(content); {
		newlineIndex := strings.IndexByte(content[cursor:], '\n')
		if newlineIndex < 0 {
			lines = append(lines, ClassifyCrontabLine(content[cursor:], ""))
			break
		}

		lineEnd := cursor + newlineIndex
		rawText := content[cursor:lineEnd]
		lineTerminator := "\n"
		if strings.HasSuffix(rawText, "\r") {
			rawText = strings.TrimSuffix(rawText, "\r")
			lineTerminator = "\r\n"
		}

		lines = append(lines, ClassifyCrontabLine(rawText, lineTerminator))
		cursor = lineEnd + 1
	}

	return lines
}

// Render 把文件寫回檔案文字。對未經修改的文件，結果與輸入 byte-for-byte 相同。
func (document *CrontabDocument) Render() string {
	var builder strings.Builder

	for _, line := range document.lines {
		builder.WriteString(line.Rendered())
	}

	return builder.String()
}

// Lines 回傳行集合的複本，避免外部改動內部狀態。
func (document *CrontabDocument) Lines() []vo.CrontabLine {
	lines := make([]vo.CrontabLine, len(document.lines))
	copy(lines, document.lines)

	return lines
}

// Jobs 解析出所有排程條目，順序與檔案中的順序一致。
func (document *CrontabDocument) Jobs() []*CronJob {
	jobs := make([]*CronJob, 0)
	seenIdentifierCounts := make(map[string]int)

	for lineIndex, line := range document.lines {
		if !line.IsJobEntry() {
			continue
		}

		job := document.buildJob(lineIndex, line, seenIdentifierCounts)
		if job == nil {
			continue
		}

		jobs = append(jobs, job)
	}

	return jobs
}

func (document *CrontabDocument) buildJob(
	lineIndex int,
	line vo.CrontabLine,
	seenIdentifierCounts map[string]int,
) *CronJob {
	entryText := strings.TrimLeft(line.RawText(), " \t")
	enabled := line.Kind() == vo.CrontabLineKindJobEntry
	if !enabled {
		entryText = strings.TrimPrefix(entryText, "#")
	}

	scheduleText, command, isJobEntry := ParseJobEntryFields(entryText)
	if !isJobEntry {
		return nil
	}

	schedule, err := NewCronSchedule(scheduleText)
	if err != nil {
		return nil
	}

	markerIdentifier, strippedRedirect := document.readMarkersAbove(lineIndex)

	origin := JobOriginForeign
	jobID := DeriveForeignJobIdentifier(schedule.Expression(), command)
	if markerIdentifier != "" {
		origin = JobOriginManaged
		jobID = markerIdentifier
	}

	seenIdentifierCounts[jobID]++
	if occurrence := seenIdentifierCounts[jobID]; occurrence > 1 {
		// 內容完全相同的兩筆條目會算出同一個摘要。加上序號後綴讓它們仍然
		// 各自可被定位，否則第二筆在 UI 上點不到。
		jobID = jobID + "-" + strconv.Itoa(occurrence)
	}

	return NewCronJob(jobID, schedule, command, origin, enabled, strippedRedirect, lineIndex)
}

// readMarkersAbove 往上讀取緊鄰的 marker 註解。
//
// 只認「連續緊鄰」的 marker：marker 與條目之間一旦夾了別的內容，就不再視為屬於
// 該條目。寧可少認一個 managed job，也不要把識別碼錯配到別人的條目上。
func (document *CrontabDocument) readMarkersAbove(lineIndex int) (markerIdentifier string, strippedRedirect string) {
	for cursor := lineIndex - 1; cursor >= 0; cursor-- {
		line := document.lines[cursor]
		if line.Kind() != vo.CrontabLineKindMarker {
			break
		}

		markerKey, markerValue, isMarker := ParseMarkerLine(line.RawText())
		if !isMarker {
			break
		}

		switch markerKey {
		case MarkerKeyIdentifier:
			markerIdentifier = markerValue
		case MarkerKeyStrippedRedirect:
			strippedRedirect = markerValue
		}
	}

	return markerIdentifier, strippedRedirect
}

// FindJob 依識別碼取出 job。
func (document *CrontabDocument) FindJob(jobID string) (*CronJob, bool) {
	for _, job := range document.Jobs() {
		if job.JobID() == jobID {
			return job, true
		}
	}

	return nil, false
}

// MustFindJob 依識別碼取出 job，找不到時回 ErrCronJobNotFound。
func (document *CrontabDocument) MustFindJob(jobID string) (*CronJob, error) {
	job, found := document.FindJob(jobID)
	if !found {
		return nil, fmt.Errorf("%w: %s", ErrCronJobNotFound, jobID)
	}

	return job, nil
}
