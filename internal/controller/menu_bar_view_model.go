package controller

import (
	"fmt"
	"strings"

	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

// menuTimeLayout 是選單列上顯示下次執行時間的格式。不含年份與秒：那一列只有
// 幾十個字元，而使用者關心的是「今天幾點」。
const menuTimeLayout = "01/02 15:04"

// 選單列上圖示旁邊的字。
//
// 一切正常時什麼都不加——那是絕大多數的時候，而一個乾淨的圖示就是使用者要的。
// 有事的時候才補一個字元，讓那一格看起來與平常不同，眼睛才會被抓過去。
//
// 刻意用形狀不同的字元而不是顏色：顏色在深色模式、在色覺差異下都可能失效，而
// 這是整個功能唯一「不點開就看得到」的資訊。
const (
	indicatorTitleNormal      = ""
	indicatorTitleAttention   = "!"
	indicatorTitleUnavailable = "?"
)

// indicatorTitleTextFallback 是畫不出圖示時的前綴。
//
// 沒有它，正常狀態會是「沒有圖示、也沒有文字」——一個看不見的選單列項目，比什麼
// 都糟。
const indicatorTitleTextFallback = "cw"

// 最近一次結果的符號。四種結果各自不同，「無從得知」尤其不能長得像成功。
const (
	outcomeSymbolSucceeded = "✓"
	outcomeSymbolFailed    = "✗"
	outcomeSymbolRunning   = "…"
	outcomeSymbolUnknown   = "—"
)

// MenuBarViewModel 是選單列要畫的東西。
//
// 它把 DTO 翻成一串現成的字串，讓平台外殼只剩下「把這些字放上去」這件事 ——
// 外殼是唯一無法用單元測試涵蓋的地方，所以它裡面的判斷越少越好。
type MenuBarViewModel struct {
	// IndicatorTitle 是選單列上那一小段文字。
	IndicatorTitle string
	// Tooltip 是滑鼠停留時的說明。
	Tooltip string

	// LineTitles 與 LineJobIDs 一一對應：點第 n 行就是要看第 n 個 job。
	LineTitles []string
	LineJobIDs []string

	// OverflowTitle 在有筆數被截掉時說明還有幾筆。沒有被截掉時為空字串。
	OverflowTitle string

	// EmptyMessage 在一筆都畫不出來時說明原因 —— 可能是真的沒有排程，也可能是
	// 根本讀不到。兩者是不同的事實，訊息必須不同。沒有這個問題時為空字串。
	EmptyMessage string
}

// NewMenuBarViewModel 把一次刷新的結果翻成選單列的內容。
func NewMenuBarViewModel(status dto.DesktopStatusDto) MenuBarViewModel {
	viewModel := MenuBarViewModel{
		IndicatorTitle: indicatorTitleOf(status),
		Tooltip:        tooltipOf(status),
		LineTitles:     make([]string, 0, len(status.Lines)),
		LineJobIDs:     make([]string, 0, len(status.Lines)),
	}

	for _, line := range status.Lines {
		viewModel.LineTitles = append(viewModel.LineTitles, buildLineTitle(line))
		viewModel.LineJobIDs = append(viewModel.LineJobIDs, line.JobID)
	}

	if status.OmittedLineCount > 0 {
		viewModel.OverflowTitle = fmt.Sprintf("另有 %d 筆…", status.OmittedLineCount)
	}

	if len(viewModel.LineTitles) == 0 {
		viewModel.EmptyMessage = emptyMessageOf(status)
	}

	return viewModel
}

func indicatorTitleOf(status dto.DesktopStatusDto) string {
	switch entity.NewStatusIndicator(status.Indicator) {
	case entity.StatusIndicatorAttention:
		return indicatorTitleAttention
	case entity.StatusIndicatorUnavailable:
		return indicatorTitleUnavailable
	default:
		return indicatorTitleNormal
	}
}

func tooltipOf(status dto.DesktopStatusDto) string {
	if entity.NewStatusIndicator(status.Indicator) == entity.StatusIndicatorUnavailable {
		return "無法取得排程：" + status.UnavailableReason
	}

	attentionCount := 0
	for _, line := range status.Lines {
		if line.NeedsAttention {
			attentionCount++
		}
	}

	if attentionCount > 0 {
		return fmt.Sprintf("有 %d 個排程最近一次沒有跑成功", attentionCount)
	}

	return "沒有已知的問題"
}

// emptyMessageOf 說明為何一筆都沒有。
//
// 「讀不到」與「真的沒有排程」必須分開講：把前者說成後者，等於在使用者的 crontab
// 明明有東西的時候告訴他那裡是空的。
func emptyMessageOf(status dto.DesktopStatusDto) string {
	if entity.NewStatusIndicator(status.Indicator) == entity.StatusIndicatorUnavailable {
		return "讀不到你的排程：" + status.UnavailableReason
	}

	return "目前沒有排程"
}

// buildLineTitle 組出一行的文字：結果符號、名稱、排程、下次執行。
//
// 已停用是掛在**名稱**上的狀態，不是下次執行欄的值 —— 把「已停用」寫進那一欄，
// 就沒有人說得出那一欄到底是什麼，看起來會像「還沒算出來」。兩件事各說各的。
func buildLineTitle(line dto.JobStatusLineDto) string {
	name := outcomeSymbolOf(line.Outcome) + " " + line.DisplayName
	if !line.Enabled {
		name += "（已停用）"
	}

	parts := []string{name, line.ScheduleDescription, nextRunTextOf(line)}

	return strings.Join(parts, " · ")
}

func outcomeSymbolOf(outcome string) string {
	switch entity.LatestRunOutcome(outcome) {
	case entity.LatestRunOutcomeSucceeded:
		return outcomeSymbolSucceeded
	case entity.LatestRunOutcomeFailed:
		return outcomeSymbolFailed
	case entity.LatestRunOutcomeRunning:
		return outcomeSymbolRunning
	default:
		return outcomeSymbolUnknown
	}
}

// nextRunTextOf 說明下次什麼時候跑。
//
// 沒有下次執行時說「不適用」而不是留白 —— 留白會被讀成「還沒算出來」。已停用的
// 條目也走這條路：它同樣沒有下次執行，而「為什麼」已經寫在名稱旁邊了。
func nextRunTextOf(line dto.JobStatusLineDto) string {
	if line.NextRunAt == nil {
		return "不適用"
	}

	return "下次 " + line.NextRunAt.Format(menuTimeLayout)
}
