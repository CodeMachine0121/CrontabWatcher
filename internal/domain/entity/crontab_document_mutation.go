package entity

import (
	"fmt"
	"strings"

	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

// defaultLineTerminator 是新增行時使用的行尾字元。
const defaultLineTerminator = "\n"

// AppendManagedJob 在檔案尾端加入一筆納管的 job，並回傳建立後的 job。
func (document *CrontabDocument) AppendManagedJob(jobID string, specification ManagedJobSpecification) (*CronJob, error) {
	document.ensureFinalLineIsTerminated()

	lineTerminator := document.dominantLineTerminator()
	appendedLines := make([]vo.CrontabLine, 0, 3)

	appendedLines = append(appendedLines, vo.NewCrontabLine(
		buildMarkerLine(MarkerKeyIdentifier, jobID), lineTerminator, vo.CrontabLineKindMarker))

	// 說明寫成 marker，而不是普通註解。普通註解無法判定所有權，刪除 job 時就只能
	// 留著它變成孤兒 —— 而它明明是我們寫的。
	if specification.Description() != "" {
		appendedLines = append(appendedLines, vo.NewCrontabLine(
			buildMarkerLine(MarkerKeyDescription, specification.Description()),
			lineTerminator, vo.CrontabLineKindMarker))
	}

	entryText := buildEntryText(
		specification.Schedule().OriginalExpression(),
		specification.BuildWrappedCommand(jobID),
		specification.Enabled(),
	)
	appendedLines = append(appendedLines, vo.NewCrontabLine(entryText, lineTerminator, entryKind(specification.Enabled())))

	document.lines = append(document.lines, appendedLines...)

	return document.MustFindJob(jobID)
}

// ReplaceJob 改寫既有 job 的條目行。
//
// managed job 保持納管（重新包上 wrapper，識別碼不變）；foreign job 保持原狀，
// 指令原樣寫入 —— 使用者只是想改排程或指令，不該因為按了儲存就被悄悄納管、
// 輸出換到別的地方去。
func (document *CrontabDocument) ReplaceJob(jobID string, specification ManagedJobSpecification) (*CronJob, error) {
	existingJob, err := document.MustFindJob(jobID)
	if err != nil {
		return nil, err
	}

	command := specification.Command()
	if existingJob.IsManaged() {
		command = specification.BuildWrappedCommand(jobID)
	}

	entryText := buildEntryText(specification.Schedule().OriginalExpression(), command, specification.Enabled())
	lineIndex := existingJob.LineIndex()
	document.lines[lineIndex] = document.lines[lineIndex].WithRawText(entryText, entryKind(specification.Enabled()))

	// 只有納管的 job 有 marker 區塊可以放說明；手寫條目不動它的周邊。
	if existingJob.IsManaged() {
		document.setDescriptionMarker(lineIndex, specification.Description())
	}

	return document.MustFindJob(document.identifierAtLine(lineIndex, jobID))
}

// RemoveJob 移除一筆 job 的條目行，以及緊鄰其上的整個 marker 區塊。
//
// 刻意不移除使用者自己寫的**普通**註解 —— 無法可靠判斷那行是屬於這個 job 還是
// 下一個，猜錯就是刪掉別人的文件。我們自己寫的說明存成 marker，正因為那樣才判得
// 出所有權、才能一起帶走。
func (document *CrontabDocument) RemoveJob(jobID string) error {
	job, err := document.MustFindJob(jobID)
	if err != nil {
		return err
	}

	// 連同緊鄰其上的整個 marker 區塊一起移除 —— 那些行（識別碼、說明、被剝離的
	// redirect）全都是我們寫的。使用者自己的說明註解不在這個區塊裡，因此不受影響。
	removalStartIndex := document.markerBlockStart(job.LineIndex())

	document.lines = append(
		document.lines[:removalStartIndex],
		document.lines[job.LineIndex()+1:]...,
	)

	return nil
}

// SetJobEnabled 啟用或停用一筆 job。
//
// 停用是在行首加上 #、保留原文；啟用是移除行首的 # 與其後最多一個空白。這讓
// 「停用再啟用」完全可逆，使用者原本的空白排版一字不改。
func (document *CrontabDocument) SetJobEnabled(jobID string, enabled bool) error {
	job, err := document.MustFindJob(jobID)
	if err != nil {
		return err
	}

	if job.Enabled() == enabled {
		return nil
	}

	lineIndex := job.LineIndex()
	rawText := document.lines[lineIndex].RawText()

	if enabled {
		rawText = strings.TrimPrefix(rawText, "#")
		rawText = strings.TrimPrefix(rawText, " ")
	} else {
		rawText = "#" + rawText
	}

	document.lines[lineIndex] = document.lines[lineIndex].WithRawText(rawText, entryKind(enabled))

	return nil
}

// AdoptJob 把一筆 foreign job 轉為納管：補上 marker、把指令包成 wrapper，並剝離
// 原本的輸出 redirect。
//
// 剝離 redirect 是必要的 —— 若保留，stdout 在到達 wrapper 之前就被導走，紀錄會
// 是空的，那比沒有紀錄更糟。被剝離的片段記在 marker 註解裡以便還原。
func (document *CrontabDocument) AdoptJob(jobID string, managedJobID string, wrapperBinaryPath string) (*CronJob, error) {
	job, err := document.MustFindJob(jobID)
	if err != nil {
		return nil, err
	}

	if job.IsManaged() {
		return nil, fmt.Errorf("%w: %s", ErrCronJobAlreadyManaged, jobID)
	}

	bareCommand, redirect := vo.ParseCommandRedirect(job.RawCommand())
	lineIndex := job.LineIndex()
	lineTerminator := document.lines[lineIndex].LineTerminator()
	if lineTerminator == "" {
		lineTerminator = document.dominantLineTerminator()
	}

	markerLines := []vo.CrontabLine{
		vo.NewCrontabLine(buildMarkerLine(MarkerKeyIdentifier, managedJobID), lineTerminator, vo.CrontabLineKindMarker),
	}
	if redirect != nil {
		markerLines = append(markerLines, vo.NewCrontabLine(
			buildMarkerLine(MarkerKeyStrippedRedirect, redirect.RawFragment()), lineTerminator, vo.CrontabLineKindMarker))
	}

	entryText := buildEntryText(
		job.Schedule().OriginalExpression(),
		buildWrappedCommand(wrapperBinaryPath, managedJobID, bareCommand),
		job.Enabled(),
	)
	document.lines[lineIndex] = document.lines[lineIndex].WithRawText(entryText, entryKind(job.Enabled()))

	document.lines = insertLines(document.lines, lineIndex, markerLines)

	return document.MustFindJob(managedJobID)
}

// setDescriptionMarker 在條目之上的 marker 區塊裡設定、更新或移除說明 marker。
//
// 說明是可有可無的，所以三種情況都要處理：新增、改寫、清空。清空時要真的把那行
// 拿掉，否則使用者刪掉說明之後檔案裡還留著一行空說明。
func (document *CrontabDocument) setDescriptionMarker(lineIndex int, description string) {
	blockStart := document.markerBlockStart(lineIndex)

	existingIndex := -1
	for cursor := blockStart; cursor < lineIndex; cursor++ {
		markerKey, _, isMarker := ParseMarkerLine(document.lines[cursor].RawText())
		if isMarker && markerKey == MarkerKeyDescription {
			existingIndex = cursor
			break
		}
	}

	if description == "" {
		if existingIndex >= 0 {
			document.lines = append(document.lines[:existingIndex], document.lines[existingIndex+1:]...)
		}
		return
	}

	markerText := buildMarkerLine(MarkerKeyDescription, description)

	if existingIndex >= 0 {
		document.lines[existingIndex] = document.lines[existingIndex].WithRawText(markerText, vo.CrontabLineKindMarker)
		return
	}

	lineTerminator := document.lines[lineIndex].LineTerminator()
	if lineTerminator == "" {
		lineTerminator = document.dominantLineTerminator()
	}

	document.lines = insertLines(document.lines, lineIndex,
		[]vo.CrontabLine{vo.NewCrontabLine(markerText, lineTerminator, vo.CrontabLineKindMarker)})
}

// ensureFinalLineIsTerminated 為原本沒有行尾的最後一行補上行尾，避免新內容被
// 黏在它後面變成同一行。
func (document *CrontabDocument) ensureFinalLineIsTerminated() {
	if len(document.lines) == 0 {
		return
	}

	lastIndex := len(document.lines) - 1
	if document.lines[lastIndex].LineTerminator() != "" {
		return
	}

	document.lines[lastIndex] = vo.NewCrontabLine(
		document.lines[lastIndex].RawText(),
		document.dominantLineTerminator(),
		document.lines[lastIndex].Kind(),
	)
}

// dominantLineTerminator 找出這份檔案主要使用的行尾字元，讓新增的行跟著它 ——
// 在 CRLF 檔案裡插進一行 LF 會讓後續的 diff 變得難讀。
func (document *CrontabDocument) dominantLineTerminator() string {
	for index := len(document.lines) - 1; index >= 0; index-- {
		if lineTerminator := document.lines[index].LineTerminator(); lineTerminator != "" {
			return lineTerminator
		}
	}

	return defaultLineTerminator
}

// identifierAtLine 回傳指定行上的 job 識別碼。foreign job 改寫後識別碼會變（它是
// 內容摘要），因此改寫完必須重新查。
func (document *CrontabDocument) identifierAtLine(lineIndex int, fallbackJobID string) string {
	for _, job := range document.Jobs() {
		if job.LineIndex() == lineIndex {
			return job.JobID()
		}
	}

	return fallbackJobID
}

func buildMarkerLine(markerKey string, markerValue string) string {
	return fmt.Sprintf("# %s%s=%s", markerPrefix, markerKey, markerValue)
}

func buildEntryText(scheduleExpression string, command string, enabled bool) string {
	entryText := fmt.Sprintf("%s %s", strings.TrimSpace(scheduleExpression), command)
	if !enabled {
		return "#" + entryText
	}

	return entryText
}

func entryKind(enabled bool) vo.CrontabLineKind {
	if enabled {
		return vo.CrontabLineKindJobEntry
	}

	return vo.CrontabLineKindDisabledJobEntry
}

func insertLines(lines []vo.CrontabLine, index int, insertedLines []vo.CrontabLine) []vo.CrontabLine {
	result := make([]vo.CrontabLine, 0, len(lines)+len(insertedLines))
	result = append(result, lines[:index]...)
	result = append(result, insertedLines...)
	result = append(result, lines[index:]...)

	return result
}
