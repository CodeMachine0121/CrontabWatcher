package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/james-hsueh/crontab-watcher/internal/application"
	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/runlog"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/shell"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/system"
)

// usageExitCode 是參數用錯時的退出碼。刻意不用 1 —— 1 太容易與「使用者的 job 失敗」
// 混淆。
const usageExitCode = 2

const runCommandUsage = `usage: cronwatch run --job=<jobId> -- <command...>

Runs a command, records the run (start, duration, exit code, output) and exits
with the command's own exit code so cron's error reporting stays truthful.

This is what managed crontab entries invoke; it is not normally run by hand.`

// runWrapperCommand 執行 wrapper subcommand。
//
// 回傳值是本程序該用的 exit code：一律是子程序的 exit code，讓 cron 看到的成敗與
// 沒有 wrapper 時完全相同。
func runWrapperCommand(arguments []string) int {
	jobID, command, err := parseRunArguments(arguments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cronwatch: %v\n\n%s\n", err, runCommandUsage)
		return usageExitCode
	}

	configuration := loadServerConfiguration()

	jobExecutionService := service.NewJobExecutionService(
		// wrapper 不讀 crontab —— 指令由 argv 帶入。這裡仍然注入 repository 是為了
		// 滿足建構子，但這條路徑不會用到它。
		buildCrontabDocumentRepository(configuration),
		runlog.NewJobRunRepository(configuration.RunRecordFilePath, configuration.RunRecordRetentionCount),
		runlog.NewJobLogRepository(),
		shell.NewCommandExecutionProxy(configuration.ShellPath),
		system.NewIdentifierGenerator(),
		system.NewClock(configuration.Location),
		configuration.RunLogDirectory,
	)

	manualTriggerApplication := application.NewManualTriggerApplication(
		jobExecutionService, configuration.ManualTriggerEnabled, configuration.ManualTriggerTimeout)

	runDto, err := manualTriggerApplication.RecordWrapperRun(context.Background(), jobID, command)
	if err != nil {
		// 我們自己沒能把執行走完。這時沒有子程序的 exit code 可以沿用，只能自己
		// 回報失敗 —— 但要說清楚是 crontab-watcher 的問題，不是 job 的問題。
		fmt.Fprintf(os.Stderr, "cronwatch: could not run job %s: %v\n", jobID, err)
		return 1
	}

	// 輸出照樣送到 stdout，讓 cron 原本的 MAILTO 行為不受影響 —— 使用者原本靠
	// 郵件收輸出的話，加了 wrapper 之後仍然收得到。
	if runDto.OutputExcerpt != "" {
		fmt.Fprint(os.Stdout, runDto.OutputExcerpt)
		if !strings.HasSuffix(runDto.OutputExcerpt, "\n") {
			fmt.Fprintln(os.Stdout)
		}
	}

	return exitCodeFromRun(runDto)
}

// exitCodeFromRun 取出該用的 exit code。
//
// 未知時回 1：wrapper 必須給出某個碼，而在無從得知的情況下宣稱成功是最糟的選擇。
func exitCodeFromRun(runDto dto.JobRunDto) int {
	if runDto.ExitCode == nil {
		return 1
	}

	if *runDto.ExitCode < 0 {
		// 被 kill 的程序沒有真正的 exit code。用 137（128+SIGKILL）表達，
		// 那是 shell 的慣例。
		return 137
	}

	return *runDto.ExitCode
}

// parseRunArguments 解析 wrapper 的參數。
//
// 只認 --job=<id> 與 -- 之後的整段指令。刻意不用 flag package：flag 會把 job 指令
// 裡以 - 開頭的參數當成自己的旗標而報錯。
func parseRunArguments(arguments []string) (jobID string, command string, err error) {
	separatorIndex := -1
	for index, argument := range arguments {
		if argument == "--" {
			separatorIndex = index
			break
		}

		if strings.HasPrefix(argument, "--job=") {
			jobID = strings.TrimPrefix(argument, "--job=")
		}
	}

	if jobID == "" {
		return "", "", fmt.Errorf("missing --job=<jobId>")
	}

	if separatorIndex < 0 {
		return "", "", fmt.Errorf("missing -- separator before the command")
	}

	commandArguments := arguments[separatorIndex+1:]
	if len(commandArguments) == 0 {
		return "", "", fmt.Errorf("no command given after --")
	}

	return jobID, joinCommandArguments(commandArguments), nil
}

// joinCommandArguments 把 argv 接回一行 shell 指令。
//
// 必須重新加引號，不能單純用空白接起來。cron 是把整行 crontab 指令交給 shell，
// shell 在 exec 我們之前就把引號吃掉了：`-- sh -c 'echo hi'` 到我們手上是三個
// argv（sh、-c、echo hi），用空白接回去會變成 `sh -c echo hi`，語意完全不同。
// 這會打壞任何帶引號的條目，而 `mysql -e "SELECT ..."` 這種寫法非常常見。
func joinCommandArguments(arguments []string) string {
	quotedArguments := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		quotedArguments = append(quotedArguments, shellQuote(argument))
	}

	return strings.Join(quotedArguments, " ")
}

// shellQuoteRequiredCharacters 是需要加引號才能安全交給 shell 的字元。
//
// 刻意**不含** `=`：加引號反而會弄壞 `FOO=bar /bin/x` 這種前置環境變數賦值 ——
// 引起來之後 shell 會把整個 FOO=bar 當成一個要執行的指令名。
const shellQuoteRequiredCharacters = " \t\n\r\"'\\$`&|;<>()*?[]{}~#!"

// shellQuote 以單引號包裝一個參數。
//
// 已經安全的參數保持原樣：這樣 UI 與 log 上顯示的指令仍然是使用者看得懂的形狀，
// 而不是滿滿的引號。
func shellQuote(argument string) string {
	if argument == "" {
		return "''"
	}

	if !strings.ContainsAny(argument, shellQuoteRequiredCharacters) {
		return argument
	}

	// 單引號內只有單引號本身需要處理：先關引號、插一個轉義的單引號、再開引號。
	return "'" + strings.ReplaceAll(argument, "'", `'\''`) + "'"
}
