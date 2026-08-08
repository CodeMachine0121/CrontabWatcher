package system_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/system"
)

func TestInstanceLockKeepsASecondInstanceOut(t *testing.T) {
	lockFilePath := filepath.Join(t.TempDir(), "state", "desktop.lock")

	firstLock, err := system.AcquireInstanceLock(lockFilePath)
	require.NoError(t, err)
	t.Cleanup(firstLock.Release)

	secondLock, err := system.AcquireInstanceLock(lockFilePath)

	assert.Nil(t, secondLock)
	assert.ErrorIs(t, err, system.ErrInstanceAlreadyRunning)
}

// 放開之後必須能重新取得。若做不到，一次乾淨的關閉就會讓使用者再也啟動不了。
func TestInstanceLockCanBeTakenAgainAfterItIsReleased(t *testing.T) {
	lockFilePath := filepath.Join(t.TempDir(), "desktop.lock")

	firstLock, err := system.AcquireInstanceLock(lockFilePath)
	require.NoError(t, err)
	firstLock.Release()

	secondLock, err := system.AcquireInstanceLock(lockFilePath)

	require.NoError(t, err)
	require.NotNil(t, secondLock)
	secondLock.Release()
}

func TestInstanceLockReleaseIsSafeToCallTwice(t *testing.T) {
	lock, err := system.AcquireInstanceLock(filepath.Join(t.TempDir(), "desktop.lock"))
	require.NoError(t, err)

	assert.NotPanics(t, lock.Release)
	assert.NotPanics(t, lock.Release)
}

func TestInstanceLockReportsADirectoryItCannotUse(t *testing.T) {
	unusableDirectory := filepath.Join(t.TempDir(), "file-not-a-directory")
	require.NoError(t, writeEmptyFile(unusableDirectory))

	lock, err := system.AcquireInstanceLock(filepath.Join(unusableDirectory, "desktop.lock"))

	assert.Nil(t, lock)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, system.ErrInstanceAlreadyRunning,
		"a broken path is not the same fact as somebody else already running")
}

func writeEmptyFile(path string) error {
	return os.WriteFile(path, nil, 0o600)
}
