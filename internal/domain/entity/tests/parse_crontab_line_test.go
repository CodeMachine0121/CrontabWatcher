package entity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	. "github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/domain/vo"
)

func TestClassifyCrontabLine(t *testing.T) {
	testCases := []struct {
		name         string
		rawText      string
		expectedKind vo.CrontabLineKind
	}{
		{name: "empty line", rawText: "", expectedKind: vo.CrontabLineKindBlank},
		{name: "whitespace only", rawText: "   \t ", expectedKind: vo.CrontabLineKindBlank},

		{name: "plain comment", rawText: "# 這是說明", expectedKind: vo.CrontabLineKindComment},
		{name: "comment without space", rawText: "#nothing to see", expectedKind: vo.CrontabLineKindComment},
		{name: "shebang-like comment", rawText: "#!/bin/bash", expectedKind: vo.CrontabLineKindComment},

		{name: "identifier marker", rawText: "# cronwatch:id=abc123", expectedKind: vo.CrontabLineKindMarker},
		{name: "identifier marker without space", rawText: "#cronwatch:id=abc123", expectedKind: vo.CrontabLineKindMarker},
		{name: "stripped redirect marker", rawText: "# cronwatch:strippedRedirect= >> /var/log/x.log", expectedKind: vo.CrontabLineKindMarker},

		{name: "shell environment", rawText: "SHELL=/bin/bash", expectedKind: vo.CrontabLineKindEnvironment},
		{name: "path environment", rawText: "PATH=/usr/local/bin:/usr/bin", expectedKind: vo.CrontabLineKindEnvironment},
		{name: "empty mailto", rawText: "MAILTO=", expectedKind: vo.CrontabLineKindEnvironment},
		{name: "underscore prefixed environment", rawText: "_MY_VAR=1", expectedKind: vo.CrontabLineKindEnvironment},

		{name: "five field entry", rawText: "0 3 * * * /bin/x", expectedKind: vo.CrontabLineKindJobEntry},
		{name: "entry with extra whitespace", rawText: "0   3  *  *  *   /bin/x --flag", expectedKind: vo.CrontabLineKindJobEntry},
		{name: "alias entry", rawText: "@daily /bin/x", expectedKind: vo.CrontabLineKindJobEntry},
		{name: "reboot entry", rawText: "@reboot /bin/x", expectedKind: vo.CrontabLineKindJobEntry},
		{name: "entry with leading whitespace", rawText: "  0 3 * * * /bin/x", expectedKind: vo.CrontabLineKindJobEntry},

		{name: "disabled entry without space", rawText: "#0 3 * * * /bin/x", expectedKind: vo.CrontabLineKindDisabledJobEntry},
		{name: "disabled entry with space", rawText: "# 0 3 * * * /bin/x", expectedKind: vo.CrontabLineKindDisabledJobEntry},
		{name: "disabled alias entry", rawText: "#@daily /bin/x", expectedKind: vo.CrontabLineKindDisabledJobEntry},
		// 已知且刻意接受的誤判：長得像排程的解說性註解會被當成停用的 job。
		// 因為 Render() 無損保留原文，誤判只影響顯示、不會破壞檔案。
		{name: "prose that happens to look like a schedule", rawText: "# 0 3 * * * 這裡以前有備份", expectedKind: vo.CrontabLineKindDisabledJobEntry},

		{name: "schedule without a command", rawText: "0 3 * * *", expectedKind: vo.CrontabLineKindComment},
		{name: "not enough fields", rawText: "0 3 * /bin/x", expectedKind: vo.CrontabLineKindComment},
		{name: "invalid schedule values", rawText: "99 3 * * * /bin/x", expectedKind: vo.CrontabLineKindComment},
		{name: "arbitrary text", rawText: "this is not a crontab line", expectedKind: vo.CrontabLineKindComment},
		{name: "alias without a command", rawText: "@daily", expectedKind: vo.CrontabLineKindComment},
		{name: "unknown alias with a command", rawText: "@fortnightly /bin/x", expectedKind: vo.CrontabLineKindComment},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			line := ClassifyCrontabLine(testCase.rawText, "\n")

			assert.Equal(t, testCase.expectedKind, line.Kind())
			assert.Equal(t, testCase.rawText, line.RawText(), "the raw text must never be altered by classification")
		})
	}
}

func TestClassifyCrontabLineNormalisesUnknownKind(t *testing.T) {
	assert.Equal(t, vo.CrontabLineKindComment, vo.NewCrontabLineKind("nonsense"))
	assert.Equal(t, vo.CrontabLineKindJobEntry, vo.NewCrontabLineKind("jobEntry"))
}
