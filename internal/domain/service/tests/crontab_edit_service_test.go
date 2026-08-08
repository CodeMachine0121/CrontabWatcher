package service_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/domain/interface/mocks"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
)

type crontabEditServiceFixture struct {
	service           *service.CrontabEditService
	crontabRepository *mocks.MockICrontabDocumentRepository
	identifiers       *mocks.MockIIdentifierGenerator
	savedContent      *string
}

func newCrontabEditServiceFixture(t *testing.T) *crontabEditServiceFixture {
	t.Helper()

	crontabRepository := mocks.NewMockICrontabDocumentRepository(t)
	identifiers := mocks.NewMockIIdentifierGenerator(t)

	return &crontabEditServiceFixture{
		service: service.NewCrontabEditService(
			crontabRepository, identifiers, wrapperBinaryPath, managedLogDirectory),
		crontabRepository: crontabRepository,
		identifiers:       identifiers,
		savedContent:      new(string),
	}
}

func (fixture *crontabEditServiceFixture) givenCrontab(content string) {
	fixture.crontabRepository.EXPECT().Load().
		Return(entity.ParseCrontabDocument(content), "fingerprint-1", nil)
}

// expectSaveCapturingContent 攔下寫入內容，讓斷言可以直接檢查最終的檔案文字。
func (fixture *crontabEditServiceFixture) expectSaveCapturingContent() {
	fixture.crontabRepository.EXPECT().Save(mock.Anything, "fingerprint-1").
		Run(func(document *entity.CrontabDocument, expectedFingerprint string) {
			*fixture.savedContent = document.Render()
		}).Return(nil)
}

func TestCreateCronJobWritesAManagedEntry(t *testing.T) {
	fixture := newCrontabEditServiceFixture(t)
	fixture.givenCrontab("0 9 * * * /bin/existing\n")
	fixture.identifiers.EXPECT().NewIdentifier().Return("job-new")
	fixture.expectSaveCapturingContent()

	jobDto, err := fixture.service.CreateCronJob("0 3 * * *", "/bin/backup.sh", "nightly", true, referenceNow)

	require.NoError(t, err)
	assert.Equal(t, "job-new", jobDto.JobID)
	assert.Equal(t, "managed", jobDto.Origin)
	assert.Equal(t, "/bin/backup.sh", jobDto.Command)
	assert.Equal(t, "managed", jobDto.LogSource)
	assert.Equal(t, "/data/logs/job-new.log", jobDto.LogFilePath)
	require.NotNil(t, jobDto.NextRunAt)

	assert.Equal(t,
		"0 9 * * * /bin/existing\n"+
			"# nightly\n"+
			"# cronwatch:id=job-new\n"+
			"0 3 * * * /app/cronwatch run --job=job-new -- /bin/backup.sh\n",
		*fixture.savedContent)
}

func TestCreateCronJobRejectsBadInputWithoutWriting(t *testing.T) {
	testCases := []struct {
		name               string
		scheduleExpression string
		command            string
		expectedError      error
	}{
		{name: "invalid schedule", scheduleExpression: "every tuesday", command: "/bin/x", expectedError: entity.ErrInvalidCronExpression},
		{name: "six field schedule", scheduleExpression: "0 0 3 * * *", command: "/bin/x", expectedError: entity.ErrInvalidCronExpression},
		{name: "blank command", scheduleExpression: "0 3 * * *", command: "  ", expectedError: entity.ErrInvalidCronCommand},
		{name: "command with a newline", scheduleExpression: "0 3 * * *", command: "/bin/x\n0 0 * * * /bin/evil", expectedError: entity.ErrInvalidCronCommand},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// 驗證失敗時連 Load 都不該發生，更不用說 Save —— mock 未設定任何期望，
			// 一旦被呼叫測試就失敗。
			fixture := newCrontabEditServiceFixture(t)

			_, err := fixture.service.CreateCronJob(
				testCase.scheduleExpression, testCase.command, "", true, referenceNow)

			assert.ErrorIs(t, err, testCase.expectedError)
		})
	}
}

func TestCreateCronJobPropagatesAFingerprintConflict(t *testing.T) {
	fixture := newCrontabEditServiceFixture(t)
	fixture.givenCrontab("")
	fixture.identifiers.EXPECT().NewIdentifier().Return("job-new")
	fixture.crontabRepository.EXPECT().Save(mock.Anything, "fingerprint-1").
		Return(entity.ErrCrontabChangedExternally)

	_, err := fixture.service.CreateCronJob("0 3 * * *", "/bin/x", "", true, referenceNow)

	assert.ErrorIs(t, err, entity.ErrCrontabChangedExternally)
}

func TestUpdateCronJobRewritesTheEntry(t *testing.T) {
	fixture := newCrontabEditServiceFixture(t)
	fixture.givenCrontab("" +
		"# keep this note\n" +
		"# cronwatch:id=job-1\n" +
		"0 3 * * * /app/cronwatch run --job=job-1 -- /bin/old\n" +
		"0 9 * * * /bin/other\n")
	fixture.expectSaveCapturingContent()

	jobDto, err := fixture.service.UpdateCronJob("job-1", "30 4 * * *", "/bin/new", "", true, referenceNow)

	require.NoError(t, err)
	assert.Equal(t, "/bin/new", jobDto.Command)
	assert.Equal(t, "30 4 * * *", jobDto.ScheduleExpression)

	assert.Equal(t,
		"# keep this note\n"+
			"# cronwatch:id=job-1\n"+
			"30 4 * * * /app/cronwatch run --job=job-1 -- /bin/new\n"+
			"0 9 * * * /bin/other\n",
		*fixture.savedContent)
}

func TestUpdateCronJobRejectsAnUnknownJob(t *testing.T) {
	fixture := newCrontabEditServiceFixture(t)
	fixture.givenCrontab("0 3 * * * /bin/x\n")

	_, err := fixture.service.UpdateCronJob("does-not-exist", "0 4 * * *", "/bin/y", "", true, referenceNow)

	assert.ErrorIs(t, err, entity.ErrCronJobNotFound)
}

func TestDeleteCronJob(t *testing.T) {
	fixture := newCrontabEditServiceFixture(t)
	fixture.givenCrontab("" +
		"# cronwatch:id=job-1\n" +
		"0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n" +
		"0 9 * * * /bin/other\n")
	fixture.expectSaveCapturingContent()

	require.NoError(t, fixture.service.DeleteCronJob("job-1"))

	assert.Equal(t, "0 9 * * * /bin/other\n", *fixture.savedContent)
}

func TestDeleteCronJobRejectsAnUnknownJob(t *testing.T) {
	fixture := newCrontabEditServiceFixture(t)
	fixture.givenCrontab("0 3 * * * /bin/x\n")

	assert.ErrorIs(t, fixture.service.DeleteCronJob("does-not-exist"), entity.ErrCronJobNotFound)
}

func TestSetCronJobEnabledIsReversibleThroughTheService(t *testing.T) {
	originalContent := "0    3 * * *   /bin/x >> /var/log/x.log 2>&1\n"

	disableFixture := newCrontabEditServiceFixture(t)
	disableFixture.givenCrontab(originalContent)
	disableFixture.expectSaveCapturingContent()

	foreignJobID := entity.ParseCrontabDocument(originalContent).Jobs()[0].JobID()

	disabledJobDto, err := disableFixture.service.SetCronJobEnabled(foreignJobID, false, referenceNow)
	require.NoError(t, err)
	assert.False(t, disabledJobDto.Enabled)
	assert.Nil(t, disabledJobDto.NextRunAt)

	disabledContent := *disableFixture.savedContent
	assert.Equal(t, "#0    3 * * *   /bin/x >> /var/log/x.log 2>&1\n", disabledContent)

	enableFixture := newCrontabEditServiceFixture(t)
	enableFixture.givenCrontab(disabledContent)
	enableFixture.expectSaveCapturingContent()

	disabledJobID := entity.ParseCrontabDocument(disabledContent).Jobs()[0].JobID()

	enabledJobDto, err := enableFixture.service.SetCronJobEnabled(disabledJobID, true, referenceNow)
	require.NoError(t, err)
	assert.True(t, enabledJobDto.Enabled)

	assert.Equal(t, originalContent, *enableFixture.savedContent,
		"enabling must restore the original line exactly, spacing included")
}

func TestAdoptCronJobWrapsAndRecordsTheStrippedRedirect(t *testing.T) {
	crontabContent := "0 3 * * * /usr/local/bin/backup.sh >> /var/log/backup.log 2>&1\n"

	fixture := newCrontabEditServiceFixture(t)
	fixture.givenCrontab(crontabContent)
	fixture.identifiers.EXPECT().NewIdentifier().Return("job-adopted")
	fixture.expectSaveCapturingContent()

	foreignJobID := entity.ParseCrontabDocument(crontabContent).Jobs()[0].JobID()

	jobDto, err := fixture.service.AdoptCronJob(foreignJobID, referenceNow)

	require.NoError(t, err)
	assert.Equal(t, "job-adopted", jobDto.JobID)
	assert.Equal(t, "managed", jobDto.Origin)
	assert.Equal(t, "/usr/local/bin/backup.sh", jobDto.Command)
	assert.Equal(t, "/data/logs/job-adopted.log", jobDto.LogFilePath,
		"output moves to the managed log; the UI must warn about this before adopting")

	assert.Equal(t,
		"# cronwatch:id=job-adopted\n"+
			"# cronwatch:strippedRedirect= >> /var/log/backup.log 2>&1\n"+
			"0 3 * * * /app/cronwatch run --job=job-adopted -- /usr/local/bin/backup.sh\n",
		*fixture.savedContent)
}

func TestAdoptCronJobRefusesAnAlreadyManagedJob(t *testing.T) {
	fixture := newCrontabEditServiceFixture(t)
	fixture.givenCrontab(managedJobCrontab)
	fixture.identifiers.EXPECT().NewIdentifier().Return("job-adopted").Maybe()

	_, err := fixture.service.AdoptCronJob("job-1", referenceNow)

	assert.ErrorIs(t, err, entity.ErrCronJobAlreadyManaged)
}

func TestAdoptCronJobRejectsAnUnknownJob(t *testing.T) {
	fixture := newCrontabEditServiceFixture(t)
	fixture.givenCrontab("0 3 * * * /bin/x\n")
	fixture.identifiers.EXPECT().NewIdentifier().Return("job-adopted").Maybe()

	_, err := fixture.service.AdoptCronJob("does-not-exist", referenceNow)

	assert.ErrorIs(t, err, entity.ErrCronJobNotFound)
}

func TestGetCrontabContentReturnsTheFileVerbatim(t *testing.T) {
	crontabContent := "# a note\n\nSHELL=/bin/sh\n0 3 * * * /bin/x\n"

	fixture := newCrontabEditServiceFixture(t)
	fixture.givenCrontab(crontabContent)

	content, err := fixture.service.GetCrontabContent()

	require.NoError(t, err)
	assert.Equal(t, crontabContent, content)
}

func TestGetCrontabContentPropagatesAReadFailure(t *testing.T) {
	fixture := newCrontabEditServiceFixture(t)
	readFailure := errors.New("permission denied")
	fixture.crontabRepository.EXPECT().Load().Return(nil, "", readFailure)

	_, err := fixture.service.GetCrontabContent()

	assert.ErrorIs(t, err, readFailure)
}
