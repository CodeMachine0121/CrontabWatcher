package vo

// discardTargetFilePath 是 shell 慣用的「丟掉輸出」目標。導到這裡等於沒有 log。
const discardTargetFilePath = "/dev/null"

// CommandRedirect 是從 crontab 指令尾端解析出來的輸出重導向。它是我們對
// 使用者手寫 job（foreign job）唯一能得知「輸出去哪了」的線索。
type CommandRedirect struct {
	targetFilePath        string
	appends               bool
	includesStandardError bool
	rawFragment           string
	trailing              bool
}

// TargetFilePath 回傳輸出被導向的檔案路徑。
func (redirect *CommandRedirect) TargetFilePath() string {
	return redirect.targetFilePath
}

// Appends 回報是 >>（附加）還是 >（覆寫）。
func (redirect *CommandRedirect) Appends() bool {
	return redirect.appends
}

// IncludesStandardError 回報 stderr 是否也被寫進同一個檔案。若否，錯誤訊息不會
// 出現在我們讀到的 log 裡 —— UI 需要據此提醒使用者。
func (redirect *CommandRedirect) IncludesStandardError() bool {
	return redirect.includesStandardError
}

// RawFragment 回傳被剝離的原始文字片段，含前導空白。adopt 時把它記在 marker
// 註解裡，才有可能一字不差地還原使用者原本的指令。
func (redirect *CommandRedirect) RawFragment() string {
	return redirect.rawFragment
}

// IsTrailing 回報這個重導向是否位於整道指令的尾端。
//
// 為 false 表示它屬於串接指令（`a && b >> log && c`）中的某一段，因此**不能**把它
// 從指令裡剝掉 —— 剝掉會讓剩下的指令變成半條 pipeline。
func (redirect *CommandRedirect) IsTrailing() bool {
	return redirect.trailing
}

// DiscardsOutput 回報輸出是否被明確丟棄（導向 /dev/null）。這種 job 有 redirect
// 但沒有 log 可讀，跟「沒有 redirect」的結果一樣。
func (redirect *CommandRedirect) DiscardsOutput() bool {
	return redirect.targetFilePath == discardTargetFilePath
}
