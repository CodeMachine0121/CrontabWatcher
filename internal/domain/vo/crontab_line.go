package vo

// CrontabLineKind 是 crontab 檔案中一行的分類。
type CrontabLineKind string

const (
	// CrontabLineKindBlank 是空行或只有空白的行。
	CrontabLineKindBlank CrontabLineKind = "blank"
	// CrontabLineKindComment 是註解行，也是所有無法辨識內容的歸屬。
	CrontabLineKindComment CrontabLineKind = "comment"
	// CrontabLineKindMarker 是本服務自己寫入的 cronwatch: 註解。
	CrontabLineKindMarker CrontabLineKind = "marker"
	// CrontabLineKindEnvironment 是 KEY=value 形式的環境變數行。
	CrontabLineKindEnvironment CrontabLineKind = "environment"
	// CrontabLineKindJobEntry 是啟用中的排程條目。
	CrontabLineKindJobEntry CrontabLineKind = "jobEntry"
	// CrontabLineKindDisabledJobEntry 是被註解掉的排程條目。
	CrontabLineKindDisabledJobEntry CrontabLineKind = "disabledJobEntry"
)

// NewCrontabLineKind 把外來字串正規化成合法的分類，無法辨識時退回註解 ——
// 註解是最安全的預設值，因為它不會讓任何行被當成可執行的排程。
func NewCrontabLineKind(value string) CrontabLineKind {
	switch CrontabLineKind(value) {
	case CrontabLineKindBlank, CrontabLineKindComment, CrontabLineKindMarker,
		CrontabLineKindEnvironment, CrontabLineKindJobEntry, CrontabLineKindDisabledJobEntry:
		return CrontabLineKind(value)
	default:
		return CrontabLineKindComment
	}
}

// CrontabLine 是 crontab 檔案中的一行：原始文字、它自己的行尾字元，以及分類。
//
// 行尾字元隨每一行各自保存，而不是整份檔案共用一個設定 —— 真實的檔案會混用
// LF 與 CRLF，也可能最後一行沒有行尾。把它存在行上，無損還原就成為結構上的
// 保證，而不是靠呼叫端小心維護。
type CrontabLine struct {
	rawText        string
	lineTerminator string
	kind           CrontabLineKind
}

// NewCrontabLine 建立一行。lineTerminator 為 "" 表示這是檔案最後一行且原本沒有
// 換行。
func NewCrontabLine(rawText string, lineTerminator string, kind CrontabLineKind) CrontabLine {
	return CrontabLine{
		rawText:        rawText,
		lineTerminator: lineTerminator,
		kind:           NewCrontabLineKind(string(kind)),
	}
}

// RawText 回傳不含行尾字元的原始文字。
func (line CrontabLine) RawText() string {
	return line.rawText
}

// LineTerminator 回傳這一行原本的行尾字元（"\n"、"\r\n" 或 ""）。
func (line CrontabLine) LineTerminator() string {
	return line.lineTerminator
}

// Kind 回傳分類。
func (line CrontabLine) Kind() CrontabLineKind {
	return line.kind
}

// Rendered 回傳這一行寫回檔案時的完整文字。
func (line CrontabLine) Rendered() string {
	return line.rawText + line.lineTerminator
}

// WithRawText 產生一個換掉文字、但保留原行尾字元的新行。改寫既有條目時用它，
// 才不會把 CRLF 檔案的某一行悄悄變成 LF。
func (line CrontabLine) WithRawText(rawText string, kind CrontabLineKind) CrontabLine {
	return CrontabLine{
		rawText:        rawText,
		lineTerminator: line.lineTerminator,
		kind:           NewCrontabLineKind(string(kind)),
	}
}

// IsJobEntry 回報這一行是否承載一個排程條目（啟用或停用皆算）。
func (line CrontabLine) IsJobEntry() bool {
	return line.kind == CrontabLineKindJobEntry || line.kind == CrontabLineKindDisabledJobEntry
}
