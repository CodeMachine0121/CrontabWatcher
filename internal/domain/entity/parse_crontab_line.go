package entity

import (
	"regexp"
	"strings"

	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

// markerPrefix 是本服務寫入 crontab 的註解前綴。
const markerPrefix = "cronwatch:"

// MarkerKeyIdentifier 與 MarkerKeyStrippedRedirect 是目前使用的兩種 marker。
const (
	MarkerKeyIdentifier       = "id"
	MarkerKeyStrippedRedirect = "strippedRedirect"
)

var (
	environmentLinePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	markerLinePattern      = regexp.MustCompile(`^#\s*` + markerPrefix + `(id|strippedRedirect)=(.*)$`)
)

// ClassifyCrontabLine 判斷一行的種類。這是無狀態的純文字轉換，故為 package-level
// 函式（比照專案對純轉換模組的既有慣例），而非掛在某個型別上。
//
// 判斷順序刻意固定：空行 → marker → 註解（含被註解掉的條目）→ 環境變數 → 條目，
// 第一個命中者勝。順序錯了會讓 marker 被當成普通註解、或讓 MAILTO= 被當成條目。
func ClassifyCrontabLine(rawText string, lineTerminator string) vo.CrontabLine {
	if strings.TrimSpace(rawText) == "" {
		return vo.NewCrontabLine(rawText, lineTerminator, vo.CrontabLineKindBlank)
	}

	trimmedText := strings.TrimLeft(rawText, " \t")

	if markerLinePattern.MatchString(trimmedText) {
		return vo.NewCrontabLine(rawText, lineTerminator, vo.CrontabLineKindMarker)
	}

	if strings.HasPrefix(trimmedText, "#") {
		uncommentedText := strings.TrimPrefix(trimmedText, "#")
		if _, _, isJobEntry := ParseJobEntryFields(uncommentedText); isJobEntry {
			return vo.NewCrontabLine(rawText, lineTerminator, vo.CrontabLineKindDisabledJobEntry)
		}
		return vo.NewCrontabLine(rawText, lineTerminator, vo.CrontabLineKindComment)
	}

	if environmentLinePattern.MatchString(trimmedText) {
		return vo.NewCrontabLine(rawText, lineTerminator, vo.CrontabLineKindEnvironment)
	}

	if _, _, isJobEntry := ParseJobEntryFields(trimmedText); isJobEntry {
		return vo.NewCrontabLine(rawText, lineTerminator, vo.CrontabLineKindJobEntry)
	}

	return vo.NewCrontabLine(rawText, lineTerminator, vo.CrontabLineKindComment)
}

// ParseJobEntryFields 把一行條目切成排程與指令兩部分。回傳的排程文字保留原始
// 空白排版，指令則是去掉前導空白後的其餘全文。
//
// isJobEntry 為 false 表示這行不是合法條目 —— 排程無法解析，或根本沒有指令。
func ParseJobEntryFields(text string) (scheduleText string, command string, isJobEntry bool) {
	trimmedText := strings.TrimLeft(text, " \t")
	if trimmedText == "" {
		return "", "", false
	}

	if strings.HasPrefix(trimmedText, "@") {
		aliasText, remainder, found := splitFirstField(trimmedText)
		if !found {
			return "", "", false
		}
		if _, err := NewCronSchedule(aliasText); err != nil {
			return "", "", false
		}
		return aliasText, remainder, true
	}

	scheduleFields := make([]string, 0, cronScheduleFieldCount)
	remainder := trimmedText
	for len(scheduleFields) < cronScheduleFieldCount {
		field, rest, found := splitFirstField(remainder)
		if field == "" {
			return "", "", false
		}
		scheduleFields = append(scheduleFields, field)
		remainder = rest
		if !found && len(scheduleFields) < cronScheduleFieldCount {
			return "", "", false
		}
	}

	if remainder == "" {
		return "", "", false
	}

	scheduleText = strings.Join(scheduleFields, " ")
	if _, err := NewCronSchedule(scheduleText); err != nil {
		return "", "", false
	}

	return scheduleText, remainder, true
}

// cronScheduleFieldCount 是標準 crontab 排程的欄位數。
const cronScheduleFieldCount = 5

// splitFirstField 取出第一個以空白分隔的 token，以及去掉其後空白的剩餘部分。
// found 表示 token 之後還有非空白內容。
func splitFirstField(text string) (field string, remainder string, found bool) {
	fieldEnd := strings.IndexAny(text, " \t")
	if fieldEnd < 0 {
		return text, "", false
	}

	remainder = strings.TrimLeft(text[fieldEnd:], " \t")

	return text[:fieldEnd], remainder, remainder != ""
}

// ParseMarkerLine 從 marker 註解行取出其鍵與值。
func ParseMarkerLine(rawText string) (markerKey string, markerValue string, isMarker bool) {
	matches := markerLinePattern.FindStringSubmatch(strings.TrimLeft(rawText, " \t"))
	if matches == nil {
		return "", "", false
	}

	return matches[1], matches[2], true
}
