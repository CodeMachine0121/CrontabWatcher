package entity_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

const wrapperBinaryPath = "/app/cronwatch"

func mustBuildSpecification(t *testing.T, scheduleExpression string, command string, description string) ManagedJobSpecification {
	t.Helper()

	specification, err := NewManagedJobSpecification(scheduleExpression, command, description, true, wrapperBinaryPath)
	require.NoError(t, err)

	return specification
}

func TestNewManagedJobSpecificationRejectsBadInput(t *testing.T) {
	testCases := []struct {
		name               string
		scheduleExpression string
		command            string
		description        string
		expectedError      error
	}{
		{name: "invalid schedule", scheduleExpression: "nope", command: "/bin/x", expectedError: ErrInvalidCronExpression},
		{name: "six field schedule", scheduleExpression: "0 0 3 * * *", command: "/bin/x", expectedError: ErrInvalidCronExpression},
		{name: "empty command", scheduleExpression: "0 3 * * *", command: "   ", expectedError: ErrInvalidCronCommand},
		{name: "command with newline", scheduleExpression: "0 3 * * *", command: "/bin/x\nrm -rf /", expectedError: ErrInvalidCronCommand},
		{name: "command with carriage return", scheduleExpression: "0 3 * * *", command: "/bin/x\rfoo", expectedError: ErrInvalidCronCommand},
		{name: "description with newline", scheduleExpression: "0 3 * * *", command: "/bin/x", description: "a\nb", expectedError: ErrInvalidCronCommand},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewManagedJobSpecification(
				testCase.scheduleExpression, testCase.command, testCase.description, true, wrapperBinaryPath)

			assert.ErrorIs(t, err, testCase.expectedError)
		})
	}
}

func TestCrontabDocumentAppendManagedJobToEmptyDocument(t *testing.T) {
	document := ParseCrontabDocument("")

	job, err := document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 3 * * *", "/bin/backup.sh", ""))
	require.NoError(t, err)

	assert.Equal(t, "# cronwatch:id=job-1\n0 3 * * * /app/cronwatch run --job=job-1 -- /bin/backup.sh\n",
		document.Render())
	assert.Equal(t, JobOriginManaged, job.Origin())
	assert.Equal(t, "/bin/backup.sh", job.InnerCommand())
}

func TestCrontabDocumentAppendManagedJobWithDescription(t *testing.T) {
	document := ParseCrontabDocument("")

	_, err := document.AppendManagedJob("job-1", mustBuildSpecification(t, "@daily", "/bin/x", "nightly rotate"))
	require.NoError(t, err)

	// 說明存成 marker 而不是普通註解：普通註解判不出所有權，刪除 job 時就只能
	// 留著它變成孤兒。
	assert.Equal(t, "# cronwatch:id=job-1\n# cronwatch:description=nightly rotate\n@daily /app/cronwatch run --job=job-1 -- /bin/x\n",
		document.Render())
	assert.Equal(t, "nightly rotate", document.Jobs()[0].Description())
}

func TestCrontabDocumentRemoveJobTakesItsDescriptionWithIt(t *testing.T) {
	document := ParseCrontabDocument("# a note the user wrote\n0 9 * * * /bin/other\n")

	created, err := document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 3 * * *", "/bin/x", "nightly"))
	require.NoError(t, err)
	require.NoError(t, document.RemoveJob(created.JobID()))

	assert.Equal(t, "# a note the user wrote\n0 9 * * * /bin/other\n", document.Render(),
		"our own description marker goes with the job; the user's own comment stays")
}

func TestCrontabDocumentReplaceJobUpdatesTheDescription(t *testing.T) {
	document := ParseCrontabDocument("")

	_, err := document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 3 * * *", "/bin/x", "original"))
	require.NoError(t, err)

	_, err = document.ReplaceJob("job-1", mustBuildSpecification(t, "0 3 * * *", "/bin/x", "revised"))
	require.NoError(t, err)

	assert.Equal(t, "# cronwatch:id=job-1\n# cronwatch:description=revised\n0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n",
		document.Render())
	assert.Equal(t, "revised", document.Jobs()[0].Description())
}

func TestCrontabDocumentReplaceJobCanClearTheDescription(t *testing.T) {
	// 清空說明要真的把那行拿掉，否則檔案裡會留著一行空說明。
	document := ParseCrontabDocument("")

	_, err := document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 3 * * *", "/bin/x", "original"))
	require.NoError(t, err)

	_, err = document.ReplaceJob("job-1", mustBuildSpecification(t, "0 3 * * *", "/bin/x", ""))
	require.NoError(t, err)

	assert.Equal(t, "# cronwatch:id=job-1\n0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n",
		document.Render())
	assert.Empty(t, document.Jobs()[0].Description())
}

func TestCrontabDocumentReplaceJobAddsADescriptionThatWasNotThere(t *testing.T) {
	document := ParseCrontabDocument("")

	_, err := document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 3 * * *", "/bin/x", ""))
	require.NoError(t, err)

	_, err = document.ReplaceJob("job-1", mustBuildSpecification(t, "0 3 * * *", "/bin/x", "added later"))
	require.NoError(t, err)

	assert.Equal(t, "# cronwatch:id=job-1\n# cronwatch:description=added later\n0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n",
		document.Render())
}

func TestCrontabDocumentAppendManagedJobLeavesExistingContentUntouched(t *testing.T) {
	originalContent := readFixture(t, "realistic_crontab.txt")
	document := ParseCrontabDocument(originalContent)

	_, err := document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 5 * * *", "/bin/new.sh", ""))
	require.NoError(t, err)

	renderedContent := document.Render()
	assert.True(t, strings.HasPrefix(renderedContent, originalContent),
		"existing content must remain byte for byte identical, with the new job appended after it")
	assert.Equal(t,
		originalContent+"# cronwatch:id=job-1\n0 5 * * * /app/cronwatch run --job=job-1 -- /bin/new.sh\n",
		renderedContent)
}

func TestCrontabDocumentAppendManagedJobAddsMissingFinalNewline(t *testing.T) {
	document := ParseCrontabDocument("0 3 * * * /bin/x")

	_, err := document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 4 * * *", "/bin/y", ""))
	require.NoError(t, err)

	assert.Equal(t,
		"0 3 * * * /bin/x\n# cronwatch:id=job-1\n0 4 * * * /app/cronwatch run --job=job-1 -- /bin/y\n",
		document.Render(),
		"the new entry must not be glued onto the previous unterminated line")
}

func TestCrontabDocumentAppendManagedJobFollowsTheFilesLineEndings(t *testing.T) {
	document := ParseCrontabDocument("0 3 * * * /bin/x\r\n")

	_, err := document.AppendManagedJob("job-1", mustBuildSpecification(t, "0 4 * * *", "/bin/y", ""))
	require.NoError(t, err)

	assert.Equal(t,
		"0 3 * * * /bin/x\r\n# cronwatch:id=job-1\r\n0 4 * * * /app/cronwatch run --job=job-1 -- /bin/y\r\n",
		document.Render())
}

func TestCrontabDocumentAppendDisabledManagedJob(t *testing.T) {
	document := ParseCrontabDocument("")

	specification, err := NewManagedJobSpecification("0 3 * * *", "/bin/x", "", false, wrapperBinaryPath)
	require.NoError(t, err)

	job, err := document.AppendManagedJob("job-1", specification)
	require.NoError(t, err)

	assert.Equal(t, "# cronwatch:id=job-1\n#0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n",
		document.Render())
	assert.False(t, job.Enabled())
}

func TestCrontabDocumentReplaceManagedJobKeepsSurroundingLines(t *testing.T) {
	document := ParseCrontabDocument("" +
		"# leading note\n" +
		"SHELL=/bin/sh\n" +
		"# cronwatch:id=job-1\n" +
		"0 3 * * * /app/cronwatch run --job=job-1 -- /bin/old.sh\n" +
		"0 9 * * * /bin/other.sh\n")

	_, err := document.ReplaceJob("job-1", mustBuildSpecification(t, "30 4 * * *", "/bin/new.sh", ""))
	require.NoError(t, err)

	assert.Equal(t, ""+
		"# leading note\n"+
		"SHELL=/bin/sh\n"+
		"# cronwatch:id=job-1\n"+
		"30 4 * * * /app/cronwatch run --job=job-1 -- /bin/new.sh\n"+
		"0 9 * * * /bin/other.sh\n",
		document.Render())
}

func TestCrontabDocumentReplaceForeignJobStaysForeign(t *testing.T) {
	// 使用者只是想改排程或指令，不該因為按了「儲存」就被悄悄納管、輸出換地方。
	document := ParseCrontabDocument("0 3 * * * /bin/x >> /var/log/x.log 2>&1\n")
	foreignJobID := document.Jobs()[0].JobID()

	_, err := document.ReplaceJob(foreignJobID,
		mustBuildSpecification(t, "0 5 * * *", "/bin/x >> /var/log/x.log 2>&1", ""))
	require.NoError(t, err)

	assert.Equal(t, "0 5 * * * /bin/x >> /var/log/x.log 2>&1\n", document.Render())
	assert.Equal(t, JobOriginForeign, document.Jobs()[0].Origin())
}

func TestCrontabDocumentReplaceUnknownJob(t *testing.T) {
	document := ParseCrontabDocument("0 3 * * * /bin/x\n")

	_, err := document.ReplaceJob("nope", mustBuildSpecification(t, "0 4 * * *", "/bin/y", ""))

	assert.ErrorIs(t, err, ErrCronJobNotFound)
}

func TestCrontabDocumentRemoveJob(t *testing.T) {
	testCases := []struct {
		name            string
		content         string
		jobIDResolver   func(document *CrontabDocument) string
		expectedContent string
	}{
		{
			name: "managed job removes its marker too",
			content: "# keep me\n" +
				"# cronwatch:id=job-1\n" +
				"0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n" +
				"0 9 * * * /bin/other.sh\n",
			jobIDResolver:   func(document *CrontabDocument) string { return "job-1" },
			expectedContent: "# keep me\n0 9 * * * /bin/other.sh\n",
		},
		{
			name: "managed job removes the stripped redirect marker too",
			content: "# cronwatch:id=job-1\n" +
				"# cronwatch:strippedRedirect= >> /var/log/x.log\n" +
				"0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n",
			jobIDResolver:   func(document *CrontabDocument) string { return "job-1" },
			expectedContent: "",
		},
		{
			name:            "foreign job removes only its own line",
			content:         "# a note about the job\n0 3 * * * /bin/x\n0 9 * * * /bin/y\n",
			jobIDResolver:   func(document *CrontabDocument) string { return document.Jobs()[0].JobID() },
			expectedContent: "# a note about the job\n0 9 * * * /bin/y\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			document := ParseCrontabDocument(testCase.content)

			require.NoError(t, document.RemoveJob(testCase.jobIDResolver(document)))

			assert.Equal(t, testCase.expectedContent, document.Render())
		})
	}
}

func TestCrontabDocumentRemoveUnknownJob(t *testing.T) {
	document := ParseCrontabDocument("0 3 * * * /bin/x\n")

	assert.ErrorIs(t, document.RemoveJob("nope"), ErrCronJobNotFound)
}

func TestCrontabDocumentSetJobEnabledIsReversible(t *testing.T) {
	originalContent := "# a note\n0    3 * * *   /bin/x >> /var/log/x.log 2>&1\nPATH=/usr/bin\n"
	document := ParseCrontabDocument(originalContent)
	jobID := document.Jobs()[0].JobID()

	require.NoError(t, document.SetJobEnabled(jobID, false))
	disabledContent := document.Render()
	assert.Equal(t, "# a note\n#0    3 * * *   /bin/x >> /var/log/x.log 2>&1\nPATH=/usr/bin\n", disabledContent)

	disabledJobID := document.Jobs()[0].JobID()
	require.False(t, document.Jobs()[0].Enabled())

	require.NoError(t, document.SetJobEnabled(disabledJobID, true))
	assert.Equal(t, originalContent, document.Render(),
		"enabling a disabled job must restore the line exactly, including its original spacing")
}

func TestCrontabDocumentSetJobEnabledIsIdempotent(t *testing.T) {
	document := ParseCrontabDocument("0 3 * * * /bin/x\n")
	jobID := document.Jobs()[0].JobID()

	require.NoError(t, document.SetJobEnabled(jobID, true))
	assert.Equal(t, "0 3 * * * /bin/x\n", document.Render())

	require.NoError(t, document.SetJobEnabled(jobID, false))
	disabledContent := document.Render()
	require.NoError(t, document.SetJobEnabled(document.Jobs()[0].JobID(), false))
	assert.Equal(t, disabledContent, document.Render())
}

func TestCrontabDocumentSetJobEnabledRemovesOnlyOneCommentMarker(t *testing.T) {
	document := ParseCrontabDocument("# 0 3 * * * /bin/x\n")
	jobID := document.Jobs()[0].JobID()

	require.NoError(t, document.SetJobEnabled(jobID, true))

	assert.Equal(t, "0 3 * * * /bin/x\n", document.Render(),
		"the single space after the hash is part of the comment syntax and goes with it")
}

func TestCrontabDocumentAdoptJob(t *testing.T) {
	document := ParseCrontabDocument("" +
		"# nightly backup\n" +
		"0 3 * * * /usr/local/bin/backup.sh >> /var/log/backup.log 2>&1\n")
	foreignJobID := document.Jobs()[0].JobID()

	adoptedJob, err := document.AdoptJob(foreignJobID, "job-1", wrapperBinaryPath)
	require.NoError(t, err)

	assert.Equal(t, ""+
		"# nightly backup\n"+
		"# cronwatch:id=job-1\n"+
		"# cronwatch:strippedRedirect= >> /var/log/backup.log 2>&1\n"+
		"0 3 * * * /app/cronwatch run --job=job-1 -- /usr/local/bin/backup.sh\n",
		document.Render())

	assert.Equal(t, JobOriginManaged, adoptedJob.Origin())
	assert.Equal(t, "/usr/local/bin/backup.sh", adoptedJob.InnerCommand())
	assert.Equal(t, " >> /var/log/backup.log 2>&1", adoptedJob.StrippedRedirect())
	assert.Equal(t, LogSourceManaged, adoptedJob.LogSource())
}

func TestCrontabDocumentAdoptJobWithoutRedirect(t *testing.T) {
	document := ParseCrontabDocument("0 3 * * * /bin/x\n")
	foreignJobID := document.Jobs()[0].JobID()

	_, err := document.AdoptJob(foreignJobID, "job-1", wrapperBinaryPath)
	require.NoError(t, err)

	assert.Equal(t, "# cronwatch:id=job-1\n0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n",
		document.Render(),
		"no stripped redirect marker when there was nothing to strip")
}

func TestCrontabDocumentAdoptJobPreservesTheDisabledState(t *testing.T) {
	document := ParseCrontabDocument("#0 3 * * * /bin/x\n")
	foreignJobID := document.Jobs()[0].JobID()

	adoptedJob, err := document.AdoptJob(foreignJobID, "job-1", wrapperBinaryPath)
	require.NoError(t, err)

	assert.Equal(t, "# cronwatch:id=job-1\n#0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n",
		document.Render())
	assert.False(t, adoptedJob.Enabled())
}

func TestCrontabDocumentAdoptAlreadyManagedJob(t *testing.T) {
	document := ParseCrontabDocument("# cronwatch:id=job-1\n0 3 * * * /app/cronwatch run --job=job-1 -- /bin/x\n")

	_, err := document.AdoptJob("job-1", "job-2", wrapperBinaryPath)

	assert.ErrorIs(t, err, ErrCronJobAlreadyManaged)
}

func TestCrontabDocumentAdoptUnknownJob(t *testing.T) {
	document := ParseCrontabDocument("0 3 * * * /bin/x\n")

	_, err := document.AdoptJob("nope", "job-1", wrapperBinaryPath)

	assert.ErrorIs(t, err, ErrCronJobNotFound)
}

func TestCrontabDocumentMutationsKeepUnrelatedBytesIdentical(t *testing.T) {
	// PRD 的關鍵驗收：整套操作跑完後，除了預期的行以外一個 byte 都不能變。
	originalContent := readFixture(t, "realistic_crontab.txt")
	document := ParseCrontabDocument(originalContent)

	createdJob, err := document.AppendManagedJob("job-new", mustBuildSpecification(t, "0 5 * * *", "/bin/new.sh", ""))
	require.NoError(t, err)

	_, err = document.ReplaceJob(createdJob.JobID(), mustBuildSpecification(t, "0 6 * * *", "/bin/new.sh --verbose", ""))
	require.NoError(t, err)

	require.NoError(t, document.SetJobEnabled(createdJob.JobID(), false))
	require.NoError(t, document.SetJobEnabled(createdJob.JobID(), true))
	require.NoError(t, document.RemoveJob(createdJob.JobID()))

	assert.Equal(t, originalContent, document.Render(),
		"after a full create/edit/disable/enable/delete round trip the file must be back to the original bytes")
}

func TestCrontabDocumentForeignIdentifierIsStableAcrossDisabling(t *testing.T) {
	// CrontabEditService 依賴這個性質：停用之後仍然用同一個識別碼找回該筆 job。
	// 摘要算的是剝掉開頭 # 之後的排程與指令，而停用只動那個 #。
	document := ParseCrontabDocument("0 3 * * * /bin/x >> /var/log/x.log\n")
	originalJobID := document.Jobs()[0].JobID()

	require.NoError(t, document.SetJobEnabled(originalJobID, false))

	assert.Equal(t, originalJobID, document.Jobs()[0].JobID())
	assert.False(t, document.Jobs()[0].Enabled())

	require.NoError(t, document.SetJobEnabled(originalJobID, true))
	assert.Equal(t, originalJobID, document.Jobs()[0].JobID())
}

func TestCrontabDocumentAdoptJobKeepsANonTrailingRedirectInPlace(t *testing.T) {
	// 串接指令中間的 redirect 剝不掉 —— 剝掉會留下半條 pipeline。所以 adopt 只是
	// 包上 wrapper：從此有真正的 exit code，輸出仍然落在使用者原本的檔案裡。
	chainedCommand := "/bin/a && /bin/b >> /var/log/b.log 2>&1 && /bin/c"
	document := ParseCrontabDocument("0 3 * * * " + chainedCommand + "\n")
	foreignJobID := document.Jobs()[0].JobID()

	adoptedJob, err := document.AdoptJob(foreignJobID, "job-1", wrapperBinaryPath)
	require.NoError(t, err)

	assert.Equal(t,
		"# cronwatch:id=job-1\n"+
			"0 3 * * * /app/cronwatch run --job=job-1 -- "+chainedCommand+"\n",
		document.Render(),
		"no stripped-redirect marker, because nothing could safely be stripped")
	assert.Empty(t, adoptedJob.StrippedRedirect())
	assert.Equal(t, chainedCommand, adoptedJob.InnerCommand())
}
