package entity_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

const managedLogDirectory = "/data/logs"

func TestCrontabDocumentExtractsForeignJobs(t *testing.T) {
	document := ParseCrontabDocument("" +
		"# nightly backup\n" +
		"0 3 * * * /usr/local/bin/backup.sh >> /var/log/backup.log 2>&1\n")

	jobs := document.Jobs()
	require.Len(t, jobs, 1)

	job := jobs[0]
	assert.Equal(t, JobOriginForeign, job.Origin())
	assert.False(t, job.IsManaged())
	assert.True(t, job.Enabled())
	assert.Len(t, job.JobID(), 12, "a foreign identifier is a 12 character digest")
	assert.Equal(t, "0 3 * * *", job.Schedule().Expression())
	assert.Equal(t, "/usr/local/bin/backup.sh >> /var/log/backup.log 2>&1", job.RawCommand())
	assert.Equal(t, "/usr/local/bin/backup.sh", job.InnerCommand())
	assert.Equal(t, 2, job.LineNumber())
}

func TestCrontabDocumentExtractsManagedJobs(t *testing.T) {
	document := ParseCrontabDocument("" +
		"# cronwatch:id=8f14e45f-ea8f-4b2c-9c3d-6a1b2c3d4e5f\n" +
		"# cronwatch:strippedRedirect= >> /var/log/report.log 2>&1\n" +
		"0 6 * * * /app/cronwatch run --job=8f14e45f-ea8f-4b2c-9c3d-6a1b2c3d4e5f -- /usr/local/bin/report.sh --daily\n")

	jobs := document.Jobs()
	require.Len(t, jobs, 1)

	job := jobs[0]
	assert.Equal(t, JobOriginManaged, job.Origin())
	assert.True(t, job.IsManaged())
	assert.Equal(t, "8f14e45f-ea8f-4b2c-9c3d-6a1b2c3d4e5f", job.JobID())
	assert.Equal(t, "/usr/local/bin/report.sh --daily", job.InnerCommand(),
		"the wrapper must be unwrapped, otherwise triggering the job would recurse into itself")
	assert.Equal(t, " >> /var/log/report.log 2>&1", job.StrippedRedirect())
	assert.Equal(t, 3, job.LineNumber())
}

func TestCrontabDocumentExtractsDisabledJobs(t *testing.T) {
	document := ParseCrontabDocument("#30 4 * * 0 /usr/local/bin/vacuum.sh\n")

	jobs := document.Jobs()
	require.Len(t, jobs, 1)

	assert.False(t, jobs[0].Enabled())
	assert.Equal(t, "30 4 * * 0", jobs[0].Schedule().Expression())
	assert.Equal(t, "/usr/local/bin/vacuum.sh", jobs[0].RawCommand())
}

func TestCrontabDocumentDerivesStableIdentifiersForForeignJobs(t *testing.T) {
	firstDocument := ParseCrontabDocument("0 3 * * * /bin/x\n")
	secondDocument := ParseCrontabDocument("# unrelated comment\n0 3 * * * /bin/x\n")

	assert.Equal(t, firstDocument.Jobs()[0].JobID(), secondDocument.Jobs()[0].JobID(),
		"the identifier depends only on schedule and command, not on position")

	differentCommand := ParseCrontabDocument("0 3 * * * /bin/y\n")
	assert.NotEqual(t, firstDocument.Jobs()[0].JobID(), differentCommand.Jobs()[0].JobID())

	differentSchedule := ParseCrontabDocument("0 4 * * * /bin/x\n")
	assert.NotEqual(t, firstDocument.Jobs()[0].JobID(), differentSchedule.Jobs()[0].JobID())
}

func TestCrontabDocumentDisambiguatesIdenticalForeignJobs(t *testing.T) {
	document := ParseCrontabDocument("0 3 * * * /bin/x\n0 3 * * * /bin/x\n")

	jobs := document.Jobs()
	require.Len(t, jobs, 2)

	assert.NotEqual(t, jobs[0].JobID(), jobs[1].JobID(),
		"identical entries must still be addressable individually")
	assert.Equal(t, jobs[0].JobID()+"-2", jobs[1].JobID())
}

func TestCronJobLogSource(t *testing.T) {
	testCases := []struct {
		name                string
		content             string
		expectedLogSource   LogSource
		expectedLogFilePath string
	}{
		{
			name:                "managed job logs to the managed directory",
			content:             "# cronwatch:id=abc\n0 3 * * * /app/cronwatch run --job=abc -- /bin/x\n",
			expectedLogSource:   LogSourceManaged,
			expectedLogFilePath: "/data/logs/abc.log",
		},
		{
			name:                "foreign job with a redirect",
			content:             "0 3 * * * /bin/x >> /var/log/x.log 2>&1\n",
			expectedLogSource:   LogSourceRedirect,
			expectedLogFilePath: "/var/log/x.log",
		},
		{
			name:                "foreign job without a redirect",
			content:             "0 3 * * * /bin/x\n",
			expectedLogSource:   LogSourceNone,
			expectedLogFilePath: "",
		},
		{
			name:                "output discarded to /dev/null is the same as no log",
			content:             "0 3 * * * /bin/x > /dev/null 2>&1\n",
			expectedLogSource:   LogSourceNone,
			expectedLogFilePath: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			jobs := ParseCrontabDocument(testCase.content).Jobs()
			require.Len(t, jobs, 1)

			assert.Equal(t, testCase.expectedLogSource, jobs[0].LogSource())
			assert.Equal(t, testCase.expectedLogFilePath, jobs[0].ResolveLogFilePath(managedLogDirectory))
		})
	}
}

func TestCronJobNextRunAt(t *testing.T) {
	document := ParseCrontabDocument("" +
		"0 3 * * * /bin/enabled\n" +
		"#0 3 * * * /bin/disabled\n" +
		"@reboot /bin/onboot\n")

	jobs := document.Jobs()
	require.Len(t, jobs, 3)

	from := time.Date(2026, 8, 8, 1, 0, 0, 0, taipeiLocation)

	enabledNextRunAt, enabledPredictable := jobs[0].NextRunAt(from)
	assert.True(t, enabledPredictable)
	assert.Equal(t, time.Date(2026, 8, 8, 3, 0, 0, 0, taipeiLocation), enabledNextRunAt)

	// 已停用的 job 不會跑。給出下次執行時間是誤導，不是貼心。
	_, disabledPredictable := jobs[1].NextRunAt(from)
	assert.False(t, disabledPredictable)

	_, rebootPredictable := jobs[2].NextRunAt(from)
	assert.False(t, rebootPredictable)
}

func TestCrontabDocumentFindJob(t *testing.T) {
	document := ParseCrontabDocument("# cronwatch:id=abc\n0 3 * * * /app/cronwatch run --job=abc -- /bin/x\n")

	foundJob, found := document.FindJob("abc")
	require.True(t, found)
	assert.Equal(t, "abc", foundJob.JobID())

	_, missing := document.FindJob("does-not-exist")
	assert.False(t, missing)
}

func TestCrontabDocumentIgnoresMarkersNotDirectlyAboveAnEntry(t *testing.T) {
	// marker 與條目之間夾了一行普通註解 —— marker 不再屬於該條目，該條目就是
	// foreign。寧可少認一個 managed job，也不要把 id 錯配到別的條目上。
	document := ParseCrontabDocument("" +
		"# cronwatch:id=abc\n" +
		"# an unrelated note\n" +
		"0 3 * * * /bin/x\n")

	jobs := document.Jobs()
	require.Len(t, jobs, 1)
	assert.Equal(t, JobOriginForeign, jobs[0].Origin())
	assert.NotEqual(t, "abc", jobs[0].JobID())
}

func TestCrontabDocumentJobsFromRealisticFixture(t *testing.T) {
	document := ParseCrontabDocument(readFixture(t, "realistic_crontab.txt"))

	jobs := document.Jobs()
	require.Len(t, jobs, 5)

	assert.Equal(t, "0 3 * * *", jobs[0].Schedule().Expression())
	assert.Equal(t, LogSourceRedirect, jobs[0].LogSource())

	assert.Equal(t, "*/15 * * * *", jobs[1].Schedule().Expression())
	assert.Equal(t, LogSourceNone, jobs[1].LogSource(), "output goes to /dev/null")

	assert.False(t, jobs[2].Enabled(), "the vacuum job is commented out")

	assert.Equal(t, "0 0 * * *", jobs[3].Schedule().Expression())
	assert.Equal(t, "@daily", jobs[3].Schedule().OriginalExpression())

	assert.False(t, jobs[4].Schedule().IsPredictable(), "@reboot has no next run")
}
