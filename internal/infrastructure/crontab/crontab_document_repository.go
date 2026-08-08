package crontab

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

const (
	// crontabFilePermission 只讓擁有者讀寫。crontab 內含會被執行的指令，等同
	// 可執行腳本，沒有理由讓其他使用者讀得到。
	crontabFilePermission = 0o600
	// crontabDirectoryPermission 同理。
	crontabDirectoryPermission = 0o700
	// absentFileFingerprint 是檔案不存在時的指紋。給它一個明確的值，這樣「檔案
	// 在我們讀完之後才被建立」也會被指紋比對抓到。
	absentFileFingerprint = "absent"
	// backupTimestampLayout 用於備份檔名，避開檔名裡的冒號。
	backupTimestampLayout = "20060102T150405.000000000"
)

// CrontabDocumentRepository 讀寫 crontab 檔案。
//
// 寫入是原子的：先寫同目錄的暫存檔，再 rename 就位。rename 在同一個檔案系統上是
// 原子操作，所以 cron 永遠不會讀到寫了一半的檔案。
type CrontabDocumentRepository struct {
	crontabFilePath string
	backupDirectory string
	writeMutex      sync.Mutex
}

// NewCrontabDocumentRepository 建立 repository。
func NewCrontabDocumentRepository(crontabFilePath string, backupDirectory string) *CrontabDocumentRepository {
	return &CrontabDocumentRepository{
		crontabFilePath: crontabFilePath,
		backupDirectory: backupDirectory,
	}
}

// Load 讀取 crontab 檔案。
//
// 檔案不存在時回傳空文件而非錯誤 —— 那是首次啟動的正常狀態，回錯誤會讓列表頁在
// 還沒建立任何 job 之前都打不開。
func (repository *CrontabDocumentRepository) Load() (*entity.CrontabDocument, string, error) {
	contentBytes, err := os.ReadFile(repository.crontabFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return entity.ParseCrontabDocument(""), absentFileFingerprint, nil
		}

		return nil, "", fmt.Errorf("reading crontab file %s: %w", repository.crontabFilePath, err)
	}

	fingerprint, err := repository.currentFingerprint()
	if err != nil {
		return nil, "", err
	}

	return entity.ParseCrontabDocument(string(contentBytes)), fingerprint, nil
}

// Save 以樂觀鎖把文件寫回檔案。
//
// 步驟順序刻意如此：先比對指紋 → 寫暫存檔 → 備份現行版本 → rename 就位。任一步
// 失敗就整體放棄，原檔不動。
func (repository *CrontabDocumentRepository) Save(document *entity.CrontabDocument, expectedFingerprint string) error {
	repository.writeMutex.Lock()
	defer repository.writeMutex.Unlock()

	currentFingerprint, err := repository.currentFingerprint()
	if err != nil {
		return err
	}

	if currentFingerprint != expectedFingerprint {
		return fmt.Errorf("%w: %s", entity.ErrCrontabChangedExternally, repository.crontabFilePath)
	}

	renderedContent := document.Render()
	if err := verifyRenderedContent(document, renderedContent); err != nil {
		return err
	}

	crontabDirectory := filepath.Dir(repository.crontabFilePath)
	if err := os.MkdirAll(crontabDirectory, crontabDirectoryPermission); err != nil {
		return fmt.Errorf("creating crontab directory %s: %w", crontabDirectory, err)
	}

	temporaryFilePath, err := repository.writeTemporaryFile(crontabDirectory, renderedContent)
	if err != nil {
		return err
	}

	if err := repository.backupCurrentVersion(); err != nil {
		_ = os.Remove(temporaryFilePath)
		return err
	}

	if err := os.Rename(temporaryFilePath, repository.crontabFilePath); err != nil {
		_ = os.Remove(temporaryFilePath)
		return fmt.Errorf("replacing crontab file %s: %w", repository.crontabFilePath, err)
	}

	return nil
}

// CrontabFilePath 回傳它所管理的檔案路徑，供錯誤訊息與 reload 使用。
func (repository *CrontabDocumentRepository) CrontabFilePath() string {
	return repository.crontabFilePath
}

// currentFingerprint 以 mtime 與大小組成版本指紋。
//
// 不用內容雜湊：crontab 檔案每次請求都會被讀，雜湊整份檔案是白花的成本，而
// mtime+size 已足以偵測人為編輯。
func (repository *CrontabDocumentRepository) currentFingerprint() (string, error) {
	fileInformation, err := os.Stat(repository.crontabFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return absentFileFingerprint, nil
		}

		return "", fmt.Errorf("inspecting crontab file %s: %w", repository.crontabFilePath, err)
	}

	return fmt.Sprintf("%d-%d", fileInformation.ModTime().UnixNano(), fileInformation.Size()), nil
}

// writeTemporaryFile 把內容寫進同目錄的暫存檔，回傳其路徑。
//
// 暫存檔必須與目標同目錄，否則 rename 會跨檔案系統而失去原子性。
func (repository *CrontabDocumentRepository) writeTemporaryFile(directory string, content string) (string, error) {
	temporaryFile, err := os.CreateTemp(directory, ".crontab-watcher-*.tmp")
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

// backupCurrentVersion 把現行檔案複製到備份目錄。檔案不存在時什麼都不做 ——
// 沒有前一版可以備份。
func (repository *CrontabDocumentRepository) backupCurrentVersion() error {
	fileInformation, err := os.Stat(repository.crontabFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("inspecting crontab file before backup: %w", err)
	}

	contentBytes, err := os.ReadFile(repository.crontabFilePath)
	if err != nil {
		return fmt.Errorf("reading crontab file before backup: %w", err)
	}

	if err := os.MkdirAll(repository.backupDirectory, crontabDirectoryPermission); err != nil {
		return fmt.Errorf("creating backup directory %s: %w", repository.backupDirectory, err)
	}

	// 用現行檔案的 mtime 當檔名，備份檔就自我說明它是哪個時間點的版本，
	// 而且不需要注入時鐘。
	backupFileName := fmt.Sprintf("crontab.%s.bak", fileInformation.ModTime().Format(backupTimestampLayout))
	backupFilePath := filepath.Join(repository.backupDirectory, backupFileName)

	if err := os.WriteFile(backupFilePath, contentBytes, crontabFilePermission); err != nil {
		return fmt.Errorf("writing backup %s: %w", backupFilePath, err)
	}

	return nil
}

// verifyRenderedContent 自我檢查：把即將寫出的內容重新解析，條目數量必須與文件
// 自己認為的一致。
//
// 這擋的是「我們在渲染時弄壞了某一行」——使用者的輸入早在 ManagedJobSpecification
// 就驗過了，這裡防的是自己的 bug。
func verifyRenderedContent(document *entity.CrontabDocument, renderedContent string) error {
	expectedJobCount := len(document.Jobs())
	actualJobCount := len(entity.ParseCrontabDocument(renderedContent).Jobs())

	if expectedJobCount != actualJobCount {
		return fmt.Errorf("refusing to write crontab: rendering changed the job count from %d to %d",
			expectedJobCount, actualJobCount)
	}

	return nil
}
