package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

// JobOrigin 區分「本服務納管的 job」與「使用者手寫的 job」。這個分野決定了我們
// 能對該 job 的執行結果知道多少。
type JobOrigin string

const (
	// JobOriginManaged 的 job 有 marker 註解，執行時經過 wrapper，因此有完整的
	// exit code 與逐次執行紀錄。
	JobOriginManaged JobOrigin = "managed"
	// JobOriginForeign 的 job 是使用者自己寫的，我們只能從 redirect 猜出 log
	// 在哪，拿不到 exit code。
	JobOriginForeign JobOrigin = "foreign"
)

// NewJobOrigin 正規化外來字串，未知值退回 foreign —— 誤判成 managed 會讓 UI
// 謊稱有完整紀錄。
func NewJobOrigin(value string) JobOrigin {
	if JobOrigin(value) == JobOriginManaged {
		return JobOriginManaged
	}
	return JobOriginForeign
}

// LogSource 說明一個 job 的輸出能從哪裡讀到。
type LogSource string

const (
	// LogSourceManaged：輸出由 wrapper 寫進本服務管理的 log 檔。
	LogSourceManaged LogSource = "managed"
	// LogSourceRedirect：輸出被指令自己的 redirect 導向某個檔案。
	LogSourceRedirect LogSource = "redirect"
	// LogSourceNone：無 log 可讀 —— 沒有 redirect，或輸出被丟到 /dev/null。
	LogSourceNone LogSource = "none"
)

// NewLogSource 正規化外來字串，未知值退回 none。
func NewLogSource(value string) LogSource {
	switch LogSource(value) {
	case LogSourceManaged, LogSourceRedirect:
		return LogSource(value)
	default:
		return LogSourceNone
	}
}

// foreignJobIdentifierLength 是 foreign job 摘要識別碼的長度。12 個 hex 字元足以
// 在個人規模下避免碰撞，又短到能放進 URL 與 UI。
const foreignJobIdentifierLength = 12

// wrapperCommandPattern 比對 wrapper 形狀的指令，用來取回內層的原指令。
var wrapperCommandPattern = regexp.MustCompile(`(?s)^\S+\s+run\s+--job=\S+\s+--\s+(.+)$`)

// CronJob 是 crontab 裡的一筆排程條目。
type CronJob struct {
	jobID            string
	schedule         *CronSchedule
	rawCommand       string
	redirect         *vo.CommandRedirect
	origin           JobOrigin
	enabled          bool
	strippedRedirect string
	description      string
	lineIndex        int
}

// NewCronJob 建立一筆排程條目。lineIndex 是它在 CrontabDocument 行集合中的索引
// （0-based），供改寫時定位。
func NewCronJob(
	jobID string,
	schedule *CronSchedule,
	rawCommand string,
	origin JobOrigin,
	enabled bool,
	strippedRedirect string,
	description string,
	lineIndex int,
) *CronJob {
	_, redirect := vo.ParseCommandRedirect(rawCommand)

	return &CronJob{
		jobID:            jobID,
		schedule:         schedule,
		rawCommand:       rawCommand,
		redirect:         redirect,
		origin:           NewJobOrigin(string(origin)),
		enabled:          enabled,
		strippedRedirect: strippedRedirect,
		description:      description,
		lineIndex:        lineIndex,
	}
}

// DeriveForeignJobIdentifier 由排程與指令算出穩定的識別碼。
//
// 這個識別碼會在使用者編輯該條目後改變，舊的執行紀錄因此變成孤兒 —— 這是刻意
// 接受的代價，替代方案是未經同意就在使用者的 crontab 裡插入 marker 註解。
func DeriveForeignJobIdentifier(scheduleExpression string, command string) string {
	digest := sha256.Sum256([]byte(scheduleExpression + "\x00" + command))

	return hex.EncodeToString(digest[:])[:foreignJobIdentifierLength]
}

// JobID 回傳識別碼。managed job 取自 marker 註解，foreign job 為內容摘要。
func (job *CronJob) JobID() string {
	return job.jobID
}

// Schedule 回傳已驗證的排程。
func (job *CronJob) Schedule() *CronSchedule {
	return job.schedule
}

// RawCommand 回傳 crontab 條目上排程之後的完整指令原文（含 wrapper 與 redirect）。
func (job *CronJob) RawCommand() string {
	return job.rawCommand
}

// InnerCommand 回傳「實際該執行的那道指令」。
//
// 剝掉 wrapper 是必要的 —— 否則手動觸發 managed job 會再次呼叫 wrapper，遞迴下去。
//
// 尾端的 redirect 也會被剝掉，否則輸出在到達我們手上之前就被導走，紀錄會是空的。
// 但**位在串接指令中間的 redirect 不剝**（`a && b >> log && c`）：剝掉會讓剩下的
// 指令變成半條 pipeline，跑起來還像成功了。這種情況下我們寧可拿不到輸出，也要
// 執行與 cron 完全相同的指令。
func (job *CronJob) InnerCommand() string {
	bareCommand, redirect := vo.ParseCommandRedirect(job.rawCommand)
	if redirect != nil && !redirect.IsTrailing() {
		bareCommand = job.rawCommand
	}

	if matches := wrapperCommandPattern.FindStringSubmatch(bareCommand); matches != nil {
		return strings.TrimSpace(matches[1])
	}

	return bareCommand
}

// Origin 回傳納管狀態。
func (job *CronJob) Origin() JobOrigin {
	return job.origin
}

// IsManaged 回報這個 job 是否由本服務納管。
func (job *CronJob) IsManaged() bool {
	return job.origin == JobOriginManaged
}

// Enabled 回報條目是否生效（未被註解掉）。
func (job *CronJob) Enabled() bool {
	return job.enabled
}

// StrippedRedirect 回傳 adopt 時被剝離、記在 marker 註解裡的原始 redirect 片段。
func (job *CronJob) StrippedRedirect() string {
	return job.strippedRedirect
}

// Description 回傳使用者為這個 job 寫的說明。只有納管的 job 才會有 —— 手寫條目
// 旁邊的註解無法可靠判定是否屬於它。
func (job *CronJob) Description() string {
	return job.description
}

// LineNumber 回傳這個條目在 crontab 檔案中的行號（1-based），供 UI 對照原檔。
func (job *CronJob) LineNumber() int {
	return job.lineIndex + 1
}

// LineIndex 回傳這個條目在行集合中的索引（0-based），供改寫時定位。
func (job *CronJob) LineIndex() int {
	return job.lineIndex
}

// Redirect 回傳指令自帶的輸出重導向，沒有則為 nil。
func (job *CronJob) Redirect() *vo.CommandRedirect {
	return job.redirect
}

// LogSource 判斷這個 job 的輸出能從哪裡讀到。
//
// 指令自帶的 redirect 優先於我們管理的 log 檔，納管與否都一樣：輸出實際上就是落在
// 那個檔案裡。這對「串接指令被 adopt」的情形特別重要 —— 那種 redirect 剝不掉，
// 所以 wrapper 只拿得到 exit code，真正的輸出仍在使用者原本的檔案。指向我們那個
// 幾乎空白的 log 只會讓人以為 job 沒有輸出。
func (job *CronJob) LogSource() LogSource {
	if job.redirect != nil && !job.redirect.DiscardsOutput() {
		return LogSourceRedirect
	}

	if job.IsManaged() {
		return LogSourceManaged
	}

	return LogSourceNone
}

// ResolveLogFilePath 給出實際要讀的 log 檔路徑。無 log 可讀時回空字串。
func (job *CronJob) ResolveLogFilePath(managedLogDirectory string) string {
	switch job.LogSource() {
	case LogSourceManaged:
		return filepath.Join(managedLogDirectory, job.jobID+".log")
	case LogSourceRedirect:
		return job.redirect.TargetFilePath()
	default:
		return ""
	}
}

// displayNameMaxRunes 是摘要清單一行放得下的名稱長度。超過就截斷 —— 一個把整
// 列擠爆的指令，讀起來反而比截斷後更難認出是哪一個 job。
const displayNameMaxRunes = 40

// DisplayName 回答「這個 job 該叫什麼名字」。
//
// 說明是使用者自己為它取的名字，最能辨識；沒有說明時指令本身就是它的身分，並
// 且用剝掉 wrapper 與 redirect 之後的那道指令 —— 那才是使用者心裡的那件事。
func (job *CronJob) DisplayName() string {
	if description := strings.TrimSpace(job.description); description != "" {
		return description
	}

	command := strings.TrimSpace(job.InnerCommand())
	if utf8.RuneCountInString(command) <= displayNameMaxRunes {
		return command
	}

	return string([]rune(command)[:displayNameMaxRunes]) + "…"
}

// NextRunAt 算出下次執行時刻。已停用的 job 一律回報無下次執行 —— 它不會跑，
// 給出時間只會誤導。
func (job *CronJob) NextRunAt(from time.Time) (time.Time, bool) {
	if !job.enabled {
		return time.Time{}, false
	}

	return job.schedule.NextRunAt(from)
}
