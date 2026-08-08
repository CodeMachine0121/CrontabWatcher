package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

// killedExitCode 是被強制收掉的程序所回報的 exit code。
//
// 用 -1 而不是某個真實的 exit code，是為了讓「被殺」在資料上就與任何正常結束
// 區分得開。
const killedExitCode = -1

// CommandExecutionProxy 以 shell 執行指令。
type CommandExecutionProxy struct {
	shellPath string
}

// NewCommandExecutionProxy 建立 proxy。
func NewCommandExecutionProxy(shellPath string) *CommandExecutionProxy {
	return &CommandExecutionProxy{shellPath: shellPath}
}

// Execute 以 shell 執行指令並回傳合併後的輸出與 exit code。
//
// 指令一律原樣交給 shell 的 -c，不做任何拼接 —— crontab 條目本身就是使用者提供的
// 完整指令，那是設計意圖，而不是注入漏洞。
//
// timeout 為 0 表示不套逾時：由 cron 排程觸發的執行該跑多久就跑多久。
//
// 回傳 error 只代表「我們沒能把這次執行走完」——指令自己失敗（非 0 exit code、
// 找不到指令）不算錯誤，那是 job 的問題，會反映在 exit code 上。
func (proxy *CommandExecutionProxy) Execute(
	ctx context.Context,
	command string,
	timeout time.Duration,
) (vo.CommandExecutionResult, error) {
	executionContext := ctx
	cancelTimeout := func() {}
	if timeout > 0 {
		executionContext, cancelTimeout = context.WithTimeout(ctx, timeout)
	}
	defer cancelTimeout()

	var outputBuffer bytes.Buffer

	// 刻意不用 exec.CommandContext：它只殺主程序，會留下孤兒子孫。我們要自己
	// 對整個 process group 下手。
	execution := exec.Command(proxy.shellPath, "-c", command)
	execution.Stdout = &outputBuffer
	// 指定同一個 buffer 讓 os/exec 共用一條 pipe，兩條流因此按實際發生順序合併。
	execution.Stderr = &outputBuffer
	execution.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := execution.Start(); err != nil {
		return vo.CommandExecutionResult{}, fmt.Errorf("starting command through %s: %w", proxy.shellPath, err)
	}

	waitCompleted := make(chan error, 1)
	go func() {
		waitCompleted <- execution.Wait()
	}()

	select {
	case waitErr := <-waitCompleted:
		return buildResultFromExit(outputBuffer.String(), execution, waitErr), nil

	case <-executionContext.Done():
		killProcessGroup(execution)
		<-waitCompleted

		if errors.Is(executionContext.Err(), context.DeadlineExceeded) && timeout > 0 {
			return vo.NewCommandExecutionResult(outputBuffer.String(), killedExitCode, true), nil
		}

		// 外部取消不是逾時。把兩者混為一談會讓紀錄謊報這個 job 跑太久。
		return vo.NewCommandExecutionResult(outputBuffer.String(), killedExitCode, false),
			fmt.Errorf("command execution cancelled: %w", executionContext.Err())
	}
}

func buildResultFromExit(output string, execution *exec.Cmd, waitErr error) vo.CommandExecutionResult {
	var exitError *exec.ExitError
	if errors.As(waitErr, &exitError) {
		return vo.NewCommandExecutionResult(output, exitError.ExitCode(), false)
	}

	if waitErr != nil {
		return vo.NewCommandExecutionResult(output, killedExitCode, false)
	}

	return vo.NewCommandExecutionResult(output, execution.ProcessState.ExitCode(), false)
}

// killProcessGroup 對整個 process group 送 SIGKILL。
//
// 對 -pgid 而不是對 pid：cron 的指令常常是 shell 一行流，會生出一串子孫程序，
// 只殺 shell 會留下還在跑的孤兒。
func killProcessGroup(execution *exec.Cmd) {
	if execution.Process == nil {
		return
	}

	if err := syscall.Kill(-execution.Process.Pid, syscall.SIGKILL); err != nil {
		// process group 已經不在了，或我們沒有權限。退回只殺主程序，總比什麼
		// 都不做好。
		_ = execution.Process.Kill()
	}
}
