package entity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

// 摘要清單一行只有幾十個字元的寬度，所以「這個 job 叫什麼」必須有個明確答案。
// 這是領域知識而不是排版細節：說明是使用者自己為這個 job 取的名字，沒有說明時
// 指令本身才是它的身分。
func TestCronJobDisplayName(t *testing.T) {
	testCases := []struct {
		name                string
		crontabContent      string
		expectedDisplayName string
	}{
		{
			name: "a managed job is named by its description",
			crontabContent: "# cronwatch:id=job-1\n" +
				"# cronwatch:description=Nightly backup\n" +
				"0 3 * * * /app/cronwatch run --job=job-1 -- /usr/local/bin/backup.sh\n",
			expectedDisplayName: "Nightly backup",
		},
		{
			name: "a managed job without a description falls back to its command",
			crontabContent: "# cronwatch:id=job-2\n" +
				"0 3 * * * /app/cronwatch run --job=job-2 -- /usr/local/bin/backup.sh\n",
			expectedDisplayName: "/usr/local/bin/backup.sh",
		},
		{
			name:                "a foreign job is named by its command, without the redirect noise",
			crontabContent:      "0 3 * * * /usr/local/bin/backup.sh >> /var/log/backup.log 2>&1\n",
			expectedDisplayName: "/usr/local/bin/backup.sh",
		},
		{
			name:                "a long command is truncated so it fits one line",
			crontabContent:      "0 3 * * * /usr/local/bin/backup.sh --target=/mnt/volumes/photos --verbose --retry=3\n",
			expectedDisplayName: "/usr/local/bin/backup.sh --target=/mnt/v…",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			jobs := ParseCrontabDocument(testCase.crontabContent).Jobs()
			require.Len(t, jobs, 1)

			assert.Equal(t, testCase.expectedDisplayName, jobs[0].DisplayName())
		})
	}
}
