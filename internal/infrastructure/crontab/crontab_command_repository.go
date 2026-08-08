package crontab

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

// contentFingerprintLength 是內容雜湊指紋取用的 hex 字元數。
const contentFingerprintLength = 16

// noCrontabIndicator 是 crontab 命令在使用者沒有 crontab 時的訊息片段。
const noCrontabIndicator = "no crontab"

// CrontabCommandRepository 透過 crontab 命令讀寫使用者的 crontab。
//
// 為什麼不直接讀檔：spool 目錄的權限是 drwx------ root:wheel，直接讀必然
// permission denied。crontab 是 setuid 執行檔，因此**它是唯一不需要 root 就能存取
// 使用者 crontab 的途徑**。替代方案是用 root 跑這個服務，而一個能執行任意指令的
// web 服務不值得為此提權。
//
// 這裡直接用 os/exec 而不經過 ICommandExecutionProxy：那個 proxy 走 shell -c 並把
// stdout 與 stderr 合併，而這裡需要分開的 stdout（crontab 內容）與 stderr（診斷
// 訊息）。直接呼叫也順帶避免了把路徑塞進 shell 字串。
type CrontabCommandRepository struct {
	crontabCommandPath string
	backupDirectory    string
	writeMutex         sync.Mutex
}

// NewCrontabCommandRepository 建立 repository。
func NewCrontabCommandRepository(crontabCommandPath string, backupDirectory string) *CrontabCommandRepository {
	return &CrontabCommandRepository{
		crontabCommandPath: crontabCommandPath,
		backupDirectory:    backupDirectory,
	}
}

// Load 以 `crontab -l` 讀取使用者的 crontab。
func (repository *CrontabCommandRepository) Load() (*entity.CrontabDocument, string, error) {
	content, _, err := repository.readCurrentContent()
	if err != nil {
		return nil, "", err
	}

	return entity.ParseCrontabDocument(content), contentFingerprint(content), nil
}

// Save 以 `crontab <file>` 安裝新的 crontab。
//
// 這是整個專案風險最高的操作：crontab <file> 會**整份取代**使用者的 crontab，弄錯
// 就是他所有排程一起消失。因此順序是：比對指紋 → 自我檢查渲染結果 → 備份現行內容
// → 寫暫存檔 → 交給 crontab 命令（它會自己驗語法）。
func (repository *CrontabCommandRepository) Save(document *entity.CrontabDocument, expectedFingerprint string) error {
	repository.writeMutex.Lock()
	defer repository.writeMutex.Unlock()

	currentContent, crontabExists, err := repository.readCurrentContent()
	if err != nil {
		return err
	}

	if contentFingerprint(currentContent) != expectedFingerprint {
		return fmt.Errorf("%w: the user crontab changed since it was read", entity.ErrCrontabChangedExternally)
	}

	renderedContent := document.Render()
	if err := verifyRenderedContent(document, renderedContent); err != nil {
		return err
	}

	if crontabExists {
		if err := repository.backupContent(currentContent); err != nil {
			return err
		}
	}

	return repository.installContent(renderedContent)
}

// CrontabSourceDescription 說明這份 crontab 是從哪裡來的，供 UI 顯示。
func (repository *CrontabCommandRepository) CrontabSourceDescription() string {
	return repository.crontabCommandPath + " -l (user crontab)"
}

// readCurrentContent 執行 `crontab -l`，回傳內容以及使用者是否已有 crontab。
//
// 沒有 crontab 時 crontab 命令回非 0 並在 stderr 說明。那是**正常狀態**，不是錯誤
// —— 回錯誤會讓還沒建立任何 job 的人連列表頁都打不開。
//
// 但「找不到 crontab 命令」必須是錯誤：靜靜地當成沒有 job，會讓使用者以為他的
// crontab 空了。
func (repository *CrontabCommandRepository) readCurrentContent() (string, bool, error) {
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer

	listCommand := exec.Command(repository.crontabCommandPath, "-l")
	listCommand.Stdout = &standardOutput
	listCommand.Stderr = &standardError

	err := listCommand.Run()
	if err == nil {
		return standardOutput.String(), true, nil
	}

	var exitError *exec.ExitError
	if !asExitError(err, &exitError) {
		return "", false, fmt.Errorf("running %s -l: %w", repository.crontabCommandPath, err)
	}

	if strings.Contains(standardError.String(), noCrontabIndicator) {
		return "", false, nil
	}

	return "", false, fmt.Errorf("%s -l failed: %s", repository.crontabCommandPath,
		describeCommandFailure(standardError.String(), err))
}

// installContent 把內容寫進暫存檔並交給 crontab 命令安裝。
func (repository *CrontabCommandRepository) installContent(content string) error {
	if err := os.MkdirAll(repository.backupDirectory, crontabDirectoryPermission); err != nil {
		return fmt.Errorf("creating working directory %s: %w", repository.backupDirectory, err)
	}

	temporaryFilePath, err := repository.writeTemporaryFile(repository.backupDirectory, content)
	if err != nil {
		return err
	}
	defer os.Remove(temporaryFilePath)

	var standardError bytes.Buffer
	installCommand := exec.Command(repository.crontabCommandPath, temporaryFilePath)
	installCommand.Stderr = &standardError

	if err := installCommand.Run(); err != nil {
		// crontab 命令自己會驗語法。它拒絕時把它的抱怨原文帶出去，而不是換成我們
		// 自己的猜測 —— 它比我們清楚為什麼不行。
		return fmt.Errorf("%s rejected the new crontab: %s", repository.crontabCommandPath,
			describeCommandFailure(standardError.String(), err))
	}

	return nil
}

// writeTemporaryFile 把內容寫進指定目錄下的暫存檔，回傳其路徑。
func (repository *CrontabCommandRepository) writeTemporaryFile(directory string, content string) (string, error) {
	temporaryFile, err := os.CreateTemp(directory, ".crontab-install-*.tmp")
	if err != nil {
		return "", fmt.Errorf("creating temporary crontab file in %s: %w", directory, err)
	}
	temporaryFilePath := temporaryFile.Name()

	writeErr := func() error {
		defer temporaryFile.Close()

		if _, err := temporaryFile.WriteString(content); err != nil {
			return fmt.Errorf("writing temporary crontab file: %w", err)
		}
		if err := temporaryFile.Chmod(crontabFilePermission); err != nil {
			return fmt.Errorf("setting permissions on temporary crontab file: %w", err)
		}

		return temporaryFile.Sync()
	}()

	if writeErr != nil {
		_ = os.Remove(temporaryFilePath)
		return "", writeErr
	}

	return temporaryFilePath, nil
}

// backupContent 把現行 crontab 內容存進備份目錄。
//
// 檔案模式用現行檔案的 mtime 當檔名；命令模式 stat 不到那個檔案，所以用備份寫入
// 當下的時間。
func (repository *CrontabCommandRepository) backupContent(content string) error {
	if err := os.MkdirAll(repository.backupDirectory, crontabDirectoryPermission); err != nil {
		return fmt.Errorf("creating backup directory %s: %w", repository.backupDirectory, err)
	}

	backupFileName := fmt.Sprintf("crontab.%s.bak", time.Now().Format(backupTimestampLayout))
	backupFilePath := filepath.Join(repository.backupDirectory, backupFileName)

	if err := os.WriteFile(backupFilePath, []byte(content), crontabFilePermission); err != nil {
		return fmt.Errorf("writing backup %s: %w", backupFilePath, err)
	}

	return nil
}

// contentFingerprint 以內容雜湊作為版本指紋。
//
// 不用 mtime+size：命令模式下我們 stat 不到那個檔案。代價是偵測不到「內容相同但
// 被重寫過」，而那本來就不需要偵測。
func contentFingerprint(content string) string {
	digest := sha256.Sum256([]byte(content))

	return hex.EncodeToString(digest[:])[:contentFingerprintLength]
}

// describeCommandFailure 優先回報命令自己印出的訊息，沒有才退回 Go 的錯誤字串。
func describeCommandFailure(standardError string, err error) string {
	trimmedStandardError := strings.TrimSpace(standardError)
	if trimmedStandardError != "" {
		return trimmedStandardError
	}

	return err.Error()
}

func asExitError(err error, target **exec.ExitError) bool {
	exitError, isExitError := err.(*exec.ExitError)
	if !isExitError {
		return false
	}

	*target = exitError

	return true
}
