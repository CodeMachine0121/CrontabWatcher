package crontab_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/crontab"
)

// 這些測試對一個**假的 crontab 腳本**執行，該腳本以一個檔案模擬 `crontab -l` 與
// `crontab <file>`。絕不碰測試執行者真正的 crontab —— 那會改動他機器上實際會跑的
// 排程。
type commandRepositoryFixture struct {
	repository      *crontab.CrontabCommandRepository
	spoolFilePath   string
	backupDirectory string
}

type fakeCrontabOptions struct {
	// hasNoCrontab 讓假腳本模擬「這個使用者沒有 crontab」（exit 1 + stderr 訊息）。
	hasNoCrontab bool
	// installFailureMessage 非空時，安裝一律失敗並印出這段訊息，模擬語法錯誤被拒。
	installFailureMessage string
}

func newCommandRepositoryFixture(t *testing.T, initialContent string, options fakeCrontabOptions) commandRepositoryFixture {
	t.Helper()

	temporaryDirectory := t.TempDir()
	spoolFilePath := filepath.Join(temporaryDirectory, "spool")
	backupDirectory := filepath.Join(temporaryDirectory, "backups")

	if !options.hasNoCrontab {
		require.NoError(t, os.WriteFile(spoolFilePath, []byte(initialContent), 0o600))
	}

	installFailure := ""
	if options.installFailureMessage != "" {
		installFailure = "echo '" + options.installFailureMessage + "' >&2\n  exit 1"
	} else {
		installFailure = "cat \"$1\" > \"$SPOOL\""
	}

	script := "#!/bin/sh\n" +
		"SPOOL=\"" + spoolFilePath + "\"\n" +
		"if [ \"$1\" = \"-l\" ]; then\n" +
		"  if [ -f \"$SPOOL\" ]; then cat \"$SPOOL\"; exit 0; fi\n" +
		"  echo \"crontab: no crontab for tester\" >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		installFailure + "\n"

	crontabCommandPath := filepath.Join(temporaryDirectory, "fake-crontab")
	require.NoError(t, os.WriteFile(crontabCommandPath, []byte(script), 0o700))

	return commandRepositoryFixture{
		repository:      crontab.NewCrontabCommandRepository(crontabCommandPath, backupDirectory),
		spoolFilePath:   spoolFilePath,
		backupDirectory: backupDirectory,
	}
}

func (fixture commandRepositoryFixture) readSpool(t *testing.T) string {
	t.Helper()

	contentBytes, err := os.ReadFile(fixture.spoolFilePath)
	require.NoError(t, err)

	return string(contentBytes)
}

func TestCommandLoadReadsTheUsersCrontab(t *testing.T) {
	content := "# a note\n0 3 * * * /bin/x >> /var/log/x.log\n"
	fixture := newCommandRepositoryFixture(t, content, fakeCrontabOptions{})

	document, fingerprint, err := fixture.repository.Load()

	require.NoError(t, err)
	assert.Equal(t, content, document.Render())
	assert.Len(t, document.Jobs(), 1)
	assert.NotEmpty(t, fingerprint)
}

func TestCommandLoadTreatsNoCrontabAsEmpty(t *testing.T) {
	// `crontab -l` 對沒有 crontab 的使用者回非 0。那是正常狀態，不是錯誤 ——
	// 回錯誤會讓還沒建立任何 job 的人連列表頁都打不開。
	fixture := newCommandRepositoryFixture(t, "", fakeCrontabOptions{hasNoCrontab: true})

	document, fingerprint, err := fixture.repository.Load()

	require.NoError(t, err)
	assert.Equal(t, "", document.Render())
	assert.Empty(t, document.Jobs())
	assert.NotEmpty(t, fingerprint)
}

func TestCommandLoadFailsWhenTheCrontabCommandIsMissing(t *testing.T) {
	// 找不到 crontab 命令是設定錯誤，不能靜靜地當成「沒有 job」—— 那會讓使用者
	// 以為他的 crontab 空了。
	repository := crontab.NewCrontabCommandRepository(
		filepath.Join(t.TempDir(), "does-not-exist"), t.TempDir())

	_, _, err := repository.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}

func TestCommandLoadSurfacesAnUnexpectedFailure(t *testing.T) {
	temporaryDirectory := t.TempDir()
	crontabCommandPath := filepath.Join(temporaryDirectory, "angry-crontab")
	require.NoError(t, os.WriteFile(crontabCommandPath,
		[]byte("#!/bin/sh\necho 'crontab: something went badly wrong' >&2\nexit 2\n"), 0o700))

	repository := crontab.NewCrontabCommandRepository(crontabCommandPath, temporaryDirectory)

	_, _, err := repository.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "something went badly wrong",
		"the command's own complaint is the most useful part of the message")
}

func TestCommandFingerprintFollowsTheContent(t *testing.T) {
	fixture := newCommandRepositoryFixture(t, "0 3 * * * /bin/x\n", fakeCrontabOptions{})

	_, firstFingerprint, err := fixture.repository.Load()
	require.NoError(t, err)

	_, secondFingerprint, err := fixture.repository.Load()
	require.NoError(t, err)
	assert.Equal(t, firstFingerprint, secondFingerprint, "unchanged content keeps its fingerprint")

	require.NoError(t, os.WriteFile(fixture.spoolFilePath, []byte("0 9 * * * /bin/y\n"), 0o600))

	_, changedFingerprint, err := fixture.repository.Load()
	require.NoError(t, err)
	assert.NotEqual(t, firstFingerprint, changedFingerprint)
}

func TestCommandSaveInstallsTheNewCrontabAndBacksUpTheOldOne(t *testing.T) {
	originalContent := "0 3 * * * /bin/old\n"
	fixture := newCommandRepositoryFixture(t, originalContent, fakeCrontabOptions{})

	document, fingerprint, err := fixture.repository.Load()
	require.NoError(t, err)

	_, err = document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 4 * * *", "/bin/new"))
	require.NoError(t, err)

	require.NoError(t, fixture.repository.Save(document, fingerprint))

	assert.Equal(t,
		originalContent+"# cronwatch:id=job-1\n0 4 * * * /app/cronwatch run --job=job-1 -- /bin/new\n",
		fixture.readSpool(t))

	backupEntries, err := os.ReadDir(fixture.backupDirectory)
	require.NoError(t, err)
	require.Len(t, backupEntries, 1)

	backupContent, err := os.ReadFile(filepath.Join(fixture.backupDirectory, backupEntries[0].Name()))
	require.NoError(t, err)
	assert.Equal(t, originalContent, string(backupContent),
		"crontab <file> replaces the whole crontab, so the previous version must be recoverable")
}

func TestCommandSaveIsVisibleToTheNextLoad(t *testing.T) {
	fixture := newCommandRepositoryFixture(t, "0 3 * * * /bin/old\n", fakeCrontabOptions{})

	document, fingerprint, err := fixture.repository.Load()
	require.NoError(t, err)
	_, err = document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 4 * * *", "/bin/new"))
	require.NoError(t, err)
	require.NoError(t, fixture.repository.Save(document, fingerprint))

	reloaded, _, err := fixture.repository.Load()
	require.NoError(t, err)
	assert.Len(t, reloaded.Jobs(), 2)
}

func TestCommandSaveRefusesToOverwriteAnExternallyChangedCrontab(t *testing.T) {
	fixture := newCommandRepositoryFixture(t, "0 3 * * * /bin/x\n", fakeCrontabOptions{})

	document, staleFingerprint, err := fixture.repository.Load()
	require.NoError(t, err)

	externalContent := "0 3 * * * /bin/x\n0 9 * * * /bin/edited-with-crontab-e\n"
	require.NoError(t, os.WriteFile(fixture.spoolFilePath, []byte(externalContent), 0o600))

	err = fixture.repository.Save(document, staleFingerprint)

	require.ErrorIs(t, err, entity.ErrCrontabChangedExternally)
	assert.Equal(t, externalContent, fixture.readSpool(t), "the hand edit must survive")
}

func TestCommandSaveSurfacesAnInstallRejection(t *testing.T) {
	// crontab 命令自己會驗語法。它拒絕時要把它的抱怨原文帶給使用者，而不是換成
	// 我們自己的猜測。
	fixture := newCommandRepositoryFixture(t, "0 3 * * * /bin/x\n",
		fakeCrontabOptions{installFailureMessage: "crontab: errors in crontab file, cannot install"})

	document, fingerprint, err := fixture.repository.Load()
	require.NoError(t, err)

	err = fixture.repository.Save(document, fingerprint)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot install")
	assert.Equal(t, "0 3 * * * /bin/x\n", fixture.readSpool(t), "the crontab is untouched")

	backupEntries, err := os.ReadDir(fixture.backupDirectory)
	require.NoError(t, err)
	for _, entry := range backupEntries {
		assert.False(t, strings.Contains(entry.Name(), ".tmp"),
			"no temporary install file may be left behind")
	}
}

func TestCommandSaveForAUserWithNoCrontabTakesNoBackup(t *testing.T) {
	fixture := newCommandRepositoryFixture(t, "", fakeCrontabOptions{hasNoCrontab: true})

	document, fingerprint, err := fixture.repository.Load()
	require.NoError(t, err)
	_, err = document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 4 * * *", "/bin/new"))
	require.NoError(t, err)

	require.NoError(t, fixture.repository.Save(document, fingerprint))

	assert.Contains(t, fixture.readSpool(t), "/bin/new")

	// 這個目錄會存在（安裝用的暫存檔放在裡面），但不該有任何備份檔 —— 本來就沒有
	// 前一版可以備份。
	backupEntries, err := os.ReadDir(fixture.backupDirectory)
	require.NoError(t, err)
	for _, entry := range backupEntries {
		assert.NotContains(t, entry.Name(), ".bak", "there was no previous version to back up")
	}
}

func TestCommandSaveIsSerialisedAcrossConcurrentCallers(t *testing.T) {
	fixture := newCommandRepositoryFixture(t, "0 3 * * * /bin/x\n", fakeCrontabOptions{})

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
			// 指紋衝突是預期結果之一：只有一個 writer 會贏。要驗的是 crontab 不會
			// 被寫成半殘。
			_ = fixture.repository.Save(document, fingerprint)
		}()
	}

	waitGroup.Wait()

	document := entity.ParseCrontabDocument(fixture.readSpool(t))
	assert.Len(t, document.Jobs(), 1)
}

func TestCommandSaveRoundTripsAFullCrudSequenceByteForByte(t *testing.T) {
	// 與檔案模式相同的關鍵驗收，只是走 crontab 命令。
	originalContent := readFixtureFile(t, "realistic_crontab.txt")
	fixture := newCommandRepositoryFixture(t, originalContent, fakeCrontabOptions{})

	document, fingerprint, err := fixture.repository.Load()
	require.NoError(t, err)
	created, err := document.AppendManagedJob("job-new", mustBuildSpecification(t, "0 5 * * *", "/bin/new.sh"))
	require.NoError(t, err)
	require.NoError(t, fixture.repository.Save(document, fingerprint))

	document, fingerprint, err = fixture.repository.Load()
	require.NoError(t, err)
	require.NoError(t, document.SetJobEnabled(created.JobID(), false))
	require.NoError(t, fixture.repository.Save(document, fingerprint))

	document, fingerprint, err = fixture.repository.Load()
	require.NoError(t, err)
	require.NoError(t, document.SetJobEnabled(created.JobID(), true))
	require.NoError(t, fixture.repository.Save(document, fingerprint))

	document, fingerprint, err = fixture.repository.Load()
	require.NoError(t, err)
	require.NoError(t, document.RemoveJob(created.JobID()))
	require.NoError(t, fixture.repository.Save(document, fingerprint))

	assert.Equal(t, originalContent, fixture.readSpool(t),
		"a full create/disable/enable/delete cycle through the crontab command leaves the file identical")
}
