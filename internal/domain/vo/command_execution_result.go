package vo

// CommandExecutionResult 是執行一道指令之後拿到的東西。
type CommandExecutionResult struct {
	output   string
	exitCode int
	timedOut bool
}

// NewCommandExecutionResult 建立執行結果。
func NewCommandExecutionResult(output string, exitCode int, timedOut bool) CommandExecutionResult {
	return CommandExecutionResult{
		output:   output,
		exitCode: exitCode,
		timedOut: timedOut,
	}
}

// Output 回傳 stdout 與 stderr 合併後的輸出。合併是刻意的 —— 使用者要判斷 job
// 出了什麼事時，兩條流分開看毫無幫助。
func (result CommandExecutionResult) Output() string {
	return result.output
}

// ExitCode 回傳離開代碼。逾時被 kill 時為 -1。
func (result CommandExecutionResult) ExitCode() int {
	return result.exitCode
}

// TimedOut 回報是否因逾時被強制收掉。
func (result CommandExecutionResult) TimedOut() bool {
	return result.timedOut
}
