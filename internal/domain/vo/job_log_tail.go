package vo

import "strings"

// JobLogTail 是從 log 檔尾端讀回來的一段內容。
type JobLogTail struct {
	content   string
	exists    bool
	truncated bool
}

// NewJobLogTail 建立一段 log 尾巴。
func NewJobLogTail(content string, exists bool, truncated bool) JobLogTail {
	return JobLogTail{
		content:   content,
		exists:    exists,
		truncated: truncated,
	}
}

// NewMissingJobLogTail 表示 log 檔還不存在。這是 job 還沒跑過的正常狀態，不是錯誤。
func NewMissingJobLogTail() JobLogTail {
	return JobLogTail{exists: false}
}

// Content 回傳 log 內容。
func (tail JobLogTail) Content() string {
	return tail.content
}

// Exists 回報 log 檔是否存在。
//
// 這個旗標必須跟內容分開回報：「檔案不存在」與「檔案存在但是空的」對使用者是
// 完全不同的兩件事，前者是還沒跑過，後者是跑過但沒有輸出。
func (tail JobLogTail) Exists() bool {
	return tail.exists
}

// Truncated 回報是否因為讀取上限而只拿到尾端一部分。
func (tail JobLogTail) Truncated() bool {
	return tail.truncated
}

// LineCount 回傳實際回傳的行數。
func (tail JobLogTail) LineCount() int {
	if tail.content == "" {
		return 0
	}

	return strings.Count(strings.TrimSuffix(tail.content, "\n"), "\n") + 1
}
