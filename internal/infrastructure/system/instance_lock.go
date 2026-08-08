package system

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrInstanceAlreadyRunning 表示這台機器上已經有一個桌面應用在跑。
var ErrInstanceAlreadyRunning = errors.New("another instance is already running")

// InstanceLock 保證同一台機器上只有一個桌面應用進駐選單列。
//
// 用檔案鎖而不是「寫一個 PID 檔再檢查那個 PID 還在不在」：鎖由作業系統在程序
// 結束時自動釋放，因此不存在「上次被強制關掉，留下一個 PID 檔擋住之後每一次
// 啟動」這種需要使用者手動清理的狀態。
type InstanceLock struct {
	file *os.File
}

// AcquireInstanceLock 取得鎖。已經有人持有時回 ErrInstanceAlreadyRunning。
func AcquireInstanceLock(lockFilePath string) (*InstanceLock, error) {
	if err := os.MkdirAll(filepath.Dir(lockFilePath), 0o755); err != nil {
		return nil, fmt.Errorf("could not prepare the lock directory: %w", err)
	}

	file, err := os.OpenFile(lockFilePath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("could not open the lock file: %w", err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()

		return nil, ErrInstanceAlreadyRunning
	}

	return &InstanceLock{file: file}, nil
}

// Release 放開鎖。程序結束時作業系統也會放開，這個方法是為了讓「還沒結束就想
// 放開」的情況（例如啟動到一半失敗）不必等程序死掉。
func (lock *InstanceLock) Release() {
	if lock == nil || lock.file == nil {
		return
	}

	_ = syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	_ = lock.file.Close()
	lock.file = nil
}
