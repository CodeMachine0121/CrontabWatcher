package entity

import (
	"fmt"
	"strings"
)

// ManagedJobSpecification 是「要把某個 job 寫成什麼樣子」的已驗證輸入。
//
// 驗證集中在建構子，所以一旦拿到 specification，就保證它寫進 crontab 不會產生
// 語法錯誤或破壞單行格式。
type ManagedJobSpecification struct {
	schedule          *CronSchedule
	command           string
	description       string
	enabled           bool
	wrapperBinaryPath string
}

// NewManagedJobSpecification 驗證並建立寫入規格。
//
// 換行字元一律拒絕：crontab 是一行一筆的格式，讓換行進到指令裡會把一筆條目變成
// 兩行，第二行可能成為一個誰都沒預期到的排程。
func NewManagedJobSpecification(
	scheduleExpression string,
	command string,
	description string,
	enabled bool,
	wrapperBinaryPath string,
) (ManagedJobSpecification, error) {
	schedule, err := NewCronSchedule(scheduleExpression)
	if err != nil {
		return ManagedJobSpecification{}, err
	}

	trimmedCommand := strings.TrimSpace(command)
	if trimmedCommand == "" {
		return ManagedJobSpecification{}, fmt.Errorf("%w: command is empty", ErrInvalidCronCommand)
	}

	if containsLineBreak(trimmedCommand) {
		return ManagedJobSpecification{}, fmt.Errorf("%w: command must not contain a line break", ErrInvalidCronCommand)
	}

	trimmedDescription := strings.TrimSpace(description)
	if containsLineBreak(trimmedDescription) {
		return ManagedJobSpecification{}, fmt.Errorf("%w: description must not contain a line break", ErrInvalidCronCommand)
	}

	return ManagedJobSpecification{
		schedule:          schedule,
		command:           trimmedCommand,
		description:       trimmedDescription,
		enabled:           enabled,
		wrapperBinaryPath: wrapperBinaryPath,
	}, nil
}

func containsLineBreak(text string) bool {
	return strings.ContainsAny(text, "\n\r")
}

// Schedule 回傳已驗證的排程。
func (specification ManagedJobSpecification) Schedule() *CronSchedule {
	return specification.schedule
}

// Command 回傳使用者輸入的指令。
func (specification ManagedJobSpecification) Command() string {
	return specification.command
}

// Description 回傳選填的說明文字。
func (specification ManagedJobSpecification) Description() string {
	return specification.description
}

// Enabled 回報這筆條目要不要生效。
func (specification ManagedJobSpecification) Enabled() bool {
	return specification.enabled
}

// WrapperBinaryPath 回傳 wrapper 執行檔的路徑。
func (specification ManagedJobSpecification) WrapperBinaryPath() string {
	return specification.wrapperBinaryPath
}

// BuildWrappedCommand 組出 managed job 在 crontab 上的指令文字。
func (specification ManagedJobSpecification) BuildWrappedCommand(jobID string) string {
	return buildWrappedCommand(specification.wrapperBinaryPath, jobID, specification.command)
}

func buildWrappedCommand(wrapperBinaryPath string, jobID string, command string) string {
	return fmt.Sprintf("%s run --job=%s -- %s", wrapperBinaryPath, jobID, command)
}
