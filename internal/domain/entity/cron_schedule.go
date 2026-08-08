package entity

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/adhocore/gronx"
)

// rebootExpression 是唯一沒有「下次執行時間」的合法排程——它綁在開機事件上，
// 而不是時間軸上。
const rebootExpression = "@reboot"

// scheduleAliasExpansions 是 crontab 的特殊字串到五欄表達式的對映。
// 刻意自己展開而不交給 gronx：這樣 Expression() 一定是實際執行者（busybox
// crond / 標準 cron）看得懂的五欄形式，UI 顯示的與現實一致。
var scheduleAliasExpansions = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

var weekdayNames = map[string]string{
	"0": "日", "1": "一", "2": "二", "3": "三",
	"4": "四", "5": "五", "6": "六", "7": "日",
}

// CronSchedule 是一個已驗證過的 cron 排程。建構成功即保證表達式合法，因此
// 系統裡不存在「不合法的 CronSchedule」。
type CronSchedule struct {
	originalExpression string
	expression         string
	predictable        bool
}

// NewCronSchedule 驗證並正規化一個 cron 表達式。
//
// 支援五欄標準表達式與 crontab 的 @ alias；刻意不支援六欄（含秒）形式，因為實際
// 執行的 cron 不吃它，接受了只會讓「下次執行時間」與現實不符。
func NewCronSchedule(expression string) (*CronSchedule, error) {
	trimmedExpression := strings.TrimSpace(expression)
	if trimmedExpression == "" {
		return nil, fmt.Errorf("%w: expression is empty", ErrInvalidCronExpression)
	}

	if strings.HasPrefix(trimmedExpression, "@") {
		return newAliasCronSchedule(expression, trimmedExpression)
	}

	normalisedExpression := strings.Join(strings.Fields(trimmedExpression), " ")
	if fieldCount := len(strings.Fields(normalisedExpression)); fieldCount != 5 {
		return nil, fmt.Errorf("%w: expected 5 fields, got %d (six-field expressions with seconds are not supported)",
			ErrInvalidCronExpression, fieldCount)
	}

	if !gronx.IsValid(normalisedExpression) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidCronExpression, trimmedExpression)
	}

	return &CronSchedule{
		originalExpression: expression,
		expression:         normalisedExpression,
		predictable:        true,
	}, nil
}

func newAliasCronSchedule(originalExpression string, trimmedExpression string) (*CronSchedule, error) {
	lowerCasedAlias := strings.ToLower(trimmedExpression)

	if lowerCasedAlias == rebootExpression {
		return &CronSchedule{
			originalExpression: originalExpression,
			expression:         rebootExpression,
			predictable:        false,
		}, nil
	}

	expandedExpression, recognised := scheduleAliasExpansions[lowerCasedAlias]
	if !recognised {
		return nil, fmt.Errorf("%w: unknown alias %q", ErrInvalidCronExpression, trimmedExpression)
	}

	return &CronSchedule{
		originalExpression: originalExpression,
		expression:         expandedExpression,
		predictable:        true,
	}, nil
}

// Expression 回傳展開並正規化後的表達式（alias 已變成五欄形式）。
func (schedule *CronSchedule) Expression() string {
	return schedule.expression
}

// OriginalExpression 回傳 crontab 檔案上的原始文字，一字不改——包含 alias 與
// 原本的空白排版。寫回檔案時要用這個，才不會把使用者的 @daily 改成 0 0 * * *。
func (schedule *CronSchedule) OriginalExpression() string {
	return schedule.originalExpression
}

// IsPredictable 回報這個排程是否有可計算的下次執行時間。只有 @reboot 沒有。
func (schedule *CronSchedule) IsPredictable() bool {
	return schedule.predictable
}

// NextRunAt 算出 from 之後的下一次執行時刻，並保持 from 的時區——cron 的排程語意
// 是綁在特定時區上的，用 UTC 硬算會得到錯誤的時間。
func (schedule *CronSchedule) NextRunAt(from time.Time) (time.Time, bool) {
	if !schedule.predictable {
		return time.Time{}, false
	}

	nextRunAt, err := gronx.NextTickAfter(schedule.expression, from, false)
	if err != nil {
		return time.Time{}, false
	}

	return nextRunAt.In(from.Location()), true
}

// Describe 給出人類可讀的排程描述。只涵蓋常見形態，其餘退回顯示原始表達式——
// 猜錯的描述比沒有描述更糟。
func (schedule *CronSchedule) Describe() string {
	if !schedule.predictable {
		return "開機時執行"
	}

	fields := strings.Fields(schedule.expression)
	minuteField, hourField := fields[0], fields[1]
	dayOfMonthField, monthField, dayOfWeekField := fields[2], fields[3], fields[4]

	if dayOfMonthField != "*" || monthField != "*" {
		return schedule.expression
	}

	if stepMinutes, isStep := parseStepValue(minuteField); isStep && hourField == "*" && dayOfWeekField == "*" {
		return fmt.Sprintf("每 %d 分鐘", stepMinutes)
	}

	minute, minuteIsNumeric := parseTimeField(minuteField)
	if !minuteIsNumeric {
		return schedule.expression
	}

	if hourField == "*" && dayOfWeekField == "*" {
		return fmt.Sprintf("每小時 %02d 分", minute)
	}

	hour, hourIsNumeric := parseTimeField(hourField)
	if !hourIsNumeric {
		return schedule.expression
	}

	if dayOfWeekField == "*" {
		return fmt.Sprintf("每天 %02d:%02d", hour, minute)
	}

	if weekdayName, isSingleWeekday := weekdayNames[dayOfWeekField]; isSingleWeekday {
		return fmt.Sprintf("每週%s %02d:%02d", weekdayName, hour, minute)
	}

	return schedule.expression
}

func parseStepValue(field string) (int, bool) {
	if !strings.HasPrefix(field, "*/") {
		return 0, false
	}

	step, err := strconv.Atoi(strings.TrimPrefix(field, "*/"))
	if err != nil {
		return 0, false
	}

	return step, true
}

func parseTimeField(field string) (int, bool) {
	value, err := strconv.Atoi(field)
	if err != nil {
		return 0, false
	}

	return value, true
}
