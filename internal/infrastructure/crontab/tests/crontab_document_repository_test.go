package crontab_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/crontab"
)

type repositoryFixture struct {
	repository      *crontab.CrontabDocumentRepository
	crontabFilePath string
	backupDirectory string
}

func newRepositoryFixture(t *testing.T, initialContent string) repositoryFixture {
	t.Helper()

	temporaryDirectory := t.TempDir()
	crontabFilePath := filepath.Join(temporaryDirectory, "crontabs", "root")
	backupDirectory := filepath.Join(temporaryDirectory, "backups")

	if initialContent != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(crontabFilePath), 0o700))
		require.NoError(t, os.WriteFile(crontabFilePath, []byte(initialContent), 0o600))
	}

	return repositoryFixture{
		repository:      crontab.NewCrontabDocumentRepository(crontabFilePath, backupDirectory),
		crontabFilePath: crontabFilePath,
		backupDirectory: backupDirectory,
	}
}

func (fixture repositoryFixture) readCrontabFile(t *testing.T) string {
	t.Helper()

	contentBytes, err := os.ReadFile(fixture.crontabFilePath)
	require.NoError(t, err)

	return string(contentBytes)
}

func TestLoadReadsTheFileAndReturnsAFingerprint(t *testing.T) {
	content := "# a note\n0 3 * * * /bin/x >> /var/log/x.log\n"
	fixture := newRepositoryFixture(t, content)

	document, fingerprint, err := fixture.repository.Load()

	require.NoError(t, err)
	assert.Equal(t, content, document.Render())
	assert.Len(t, document.Jobs(), 1)
	assert.NotEmpty(t, fingerprint)
}

func TestLoadTreatsAMissingFileAsAnEmptyDocument(t *testing.T) {
	// 首次啟動時檔案還不存在。這是正常狀態，不是錯誤 —— 回錯誤會讓整個列表頁
	// 在還沒建立任何 job 之前都打不開。
	fixture := newRepositoryFixture(t, "")

	document, fingerprint, err := fixture.repository.Load()

	require.NoError(t, err)
	assert.Equal(t, "", document.Render())
	assert.Empty(t, document.Jobs())
	assert.NotEmpty(t, fingerprint, "an absent file still has a fingerprint, so writes can detect it appearing")
}

func TestSaveWritesTheDocumentAndBacksUpThePreviousVersion(t *testing.T) {
	originalContent := "0 3 * * * /bin/old\n"
	fixture := newRepositoryFixture(t, originalContent)

	document, fingerprint, err := fixture.repository.Load()
	require.NoError(t, err)

	_, err = document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 4 * * *", "/bin/new"))
	require.NoError(t, err)

	require.NoError(t, fixture.repository.Save(document, fingerprint))

	assert.Equal(t,
		originalContent+"# cronwatch:id=job-1\n0 4 * * * /app/cronwatch run --job=job-1 -- /bin/new\n",
		fixture.readCrontabFile(t))

	backupEntries, err := os.ReadDir(fixture.backupDirectory)
	require.NoError(t, err)
	require.Len(t, backupEntries, 1, "exactly one backup for one write")

	backupContent, err := os.ReadFile(filepath.Join(fixture.backupDirectory, backupEntries[0].Name()))
	require.NoError(t, err)
	assert.Equal(t, originalContent, string(backupContent), "the backup holds the version from before the write")
}

func TestSaveCreatesTheFileAndItsParentDirectoryWhenAbsent(t *testing.T) {
	fixture := newRepositoryFixture(t, "")

	document, fingerprint, err := fixture.repository.Load()
	require.NoError(t, err)

	_, err = document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 4 * * *", "/bin/new"))
	require.NoError(t, err)

	require.NoError(t, fixture.repository.Save(document, fingerprint))

	assert.Equal(t,
		"# cronwatch:id=job-1\n0 4 * * * /app/cronwatch run --job=job-1 -- /bin/new\n",
		fixture.readCrontabFile(t))

	// 沒有前一版可備份，所以備份目錄也還不需要存在 —— 它是用到才建立的。
	_, err = os.Stat(fixture.backupDirectory)
	assert.True(t, os.IsNotExist(err), "the backup directory is created lazily, only when there is something to back up")
}

func TestSaveKeepsTheFileReadableOnlyByItsOwner(t *testing.T) {
	// crontab 內含指令，等同可執行的腳本。讓其他使用者讀得到是沒有理由的風險。
	fixture := newRepositoryFixture(t, "0 3 * * * /bin/x\n")

	document, fingerprint, err := fixture.repository.Load()
	require.NoError(t, err)
	require.NoError(t, fixture.repository.Save(document, fingerprint))

	fileInformation, err := os.Stat(fixture.crontabFilePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInformation.Mode().Perm())
}

func TestSaveRefusesToOverwriteAnExternallyChangedFile(t *testing.T) {
	// 使用者可能同時在跑 crontab -e。默默吃掉他的手改是最糟的結果。
	fixture := newRepositoryFixture(t, "0 3 * * * /bin/x\n")

	document, staleFingerprint, err := fixture.repository.Load()
	require.NoError(t, err)

	externalContent := "0 3 * * * /bin/x\n0 9 * * * /bin/edited-by-hand\n"
	require.NoError(t, os.WriteFile(fixture.crontabFilePath, []byte(externalContent), 0o600))

	err = fixture.repository.Save(document, staleFingerprint)

	require.ErrorIs(t, err, entity.ErrCrontabChangedExternally)
	assert.Equal(t, externalContent, fixture.readCrontabFile(t),
		"the hand edit must survive untouched")
}

func TestSaveSucceedsAfterReloadingTheChangedFile(t *testing.T) {
	fixture := newRepositoryFixture(t, "0 3 * * * /bin/x\n")

	require.NoError(t, os.WriteFile(fixture.crontabFilePath, []byte("0 9 * * * /bin/y\n"), 0o600))

	document, freshFingerprint, err := fixture.repository.Load()
	require.NoError(t, err)

	_, err = document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 4 * * *", "/bin/new"))
	require.NoError(t, err)

	require.NoError(t, fixture.repository.Save(document, freshFingerprint))
	assert.Contains(t, fixture.readCrontabFile(t), "/bin/y")
}

func TestSaveLeavesNoTemporaryFileBehindWhenItFails(t *testing.T) {
	fixture := newRepositoryFixture(t, "0 3 * * * /bin/x\n")

	document, fingerprint, err := fixture.repository.Load()
	require.NoError(t, err)

	crontabDirectory := filepath.Dir(fixture.crontabFilePath)
	require.NoError(t, os.Chmod(crontabDirectory, 0o500))
	t.Cleanup(func() { _ = os.Chmod(crontabDirectory, 0o700) })

	err = fixture.repository.Save(document, fingerprint)
	require.Error(t, err)

	directoryEntries, err := os.ReadDir(crontabDirectory)
	require.NoError(t, err)
	assert.Len(t, directoryEntries, 1, "only the original crontab file should remain")
	assert.Equal(t, "root", directoryEntries[0].Name())
}

func TestSaveIsSerialisedAcrossConcurrentCallers(t *testing.T) {
	fixture := newRepositoryFixture(t, "0 3 * * * /bin/x\n")

	const concurrentWriterCount = 8
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrentWriterCount)

	for writerIndex := 0; writerIndex < concurrentWriterCount; writerIndex++ {
		go func() {
			defer waitGroup.Done()

			document, fingerprint, err := fixture.repository.Load()
			if err != nil {
				return
			}
			// 指紋衝突是預期結果之一：只有一個 writer 能贏。這裡要驗證的是檔案
			// 不會被寫成半殘，而不是每個 writer 都成功。
			_ = fixture.repository.Save(document, fingerprint)
		}()
	}

	waitGroup.Wait()

	document := entity.ParseCrontabDocument(fixture.readCrontabFile(t))
	assert.Len(t, document.Jobs(), 1, "the file is still a valid crontab with its single job")
}

func TestSaveAcceptsDeliberatelyEmptyingTheCrontab(t *testing.T) {
	// 刪掉最後一個 job 之後檔案就是空的。這是合法的寫入，不是「內容遺失」——
	// Save 的自我檢查比對的是文件自己認為的條目數，而非「不得為零」。
	fixture := newRepositoryFixture(t, "0 3 * * * /bin/x\n")

	document, fingerprint, err := fixture.repository.Load()
	require.NoError(t, err)
	require.NoError(t, document.RemoveJob(document.Jobs()[0].JobID()))

	require.NoError(t, fixture.repository.Save(document, fingerprint))

	assert.Equal(t, "", fixture.readCrontabFile(t))
}
