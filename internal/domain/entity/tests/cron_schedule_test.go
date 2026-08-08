package entity_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

var taipeiLocation = mustLoadLocation("Asia/Taipei")

func mustLoadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return location
}

func TestNewCronScheduleRejectsInvalidExpressions(t *testing.T) {
	testCases := []struct {
		name       string
		expression string
	}{
		{name: "gibberish", expression: "not a cron"},
		{name: "empty", expression: ""},
		{name: "whitespace only", expression: "   "},
		{name: "four fields", expression: "0 3 * *"},
		{name: "six fields is deliberately unsupported", expression: "0 0 3 * * *"},
		{name: "unknown alias", expression: "@fortnightly"},
		{name: "minute out of range", expression: "99 3 * * *"},
		{name: "hour out of range", expression: "0 25 * * *"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			schedule, err := NewCronSchedule(testCase.expression)

			require.ErrorIs(t, err, ErrInvalidCronExpression)
			assert.Nil(t, schedule)
		})
	}
}

func TestCronScheduleNextRunAt(t *testing.T) {
	testCases := []struct {
		name       string
		expression string
		from       time.Time
		expected   time.Time
	}{
		{
			name:       "later the same day",
			expression: "0 3 * * *",
			from:       time.Date(2026, 8, 8, 1, 0, 0, 0, taipeiLocation),
			expected:   time.Date(2026, 8, 8, 3, 0, 0, 0, taipeiLocation),
		},
		{
			name:       "rolls over to the next day",
			expression: "0 3 * * *",
			from:       time.Date(2026, 8, 8, 5, 0, 0, 0, taipeiLocation),
			expected:   time.Date(2026, 8, 9, 3, 0, 0, 0, taipeiLocation),
		},
		{
			name:       "step minutes",
			expression: "*/15 * * * *",
			from:       time.Date(2026, 8, 8, 1, 7, 0, 0, taipeiLocation),
			expected:   time.Date(2026, 8, 8, 1, 15, 0, 0, taipeiLocation),
		},
		{
			name:       "rolls over to the next year",
			expression: "0 0 1 1 *",
			from:       time.Date(2026, 8, 8, 12, 0, 0, 0, taipeiLocation),
			expected:   time.Date(2027, 1, 1, 0, 0, 0, 0, taipeiLocation),
		},
		{
			name:       "weekday range skips the weekend",
			expression: "0 0 * * 1-5",
			from:       time.Date(2026, 8, 8, 12, 0, 0, 0, taipeiLocation), // 2026-08-08 is a Saturday
			expected:   time.Date(2026, 8, 10, 0, 0, 0, 0, taipeiLocation),
		},
		{
			name:       "daily alias",
			expression: "@daily",
			from:       time.Date(2026, 8, 8, 12, 0, 0, 0, taipeiLocation),
			expected:   time.Date(2026, 8, 9, 0, 0, 0, 0, taipeiLocation),
		},
		{
			name:       "hourly alias",
			expression: "@hourly",
			from:       time.Date(2026, 8, 8, 12, 30, 0, 0, taipeiLocation),
			expected:   time.Date(2026, 8, 8, 13, 0, 0, 0, taipeiLocation),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			schedule, err := NewCronSchedule(testCase.expression)
			require.NoError(t, err)

			nextRunAt, predictable := schedule.NextRunAt(testCase.from)

			require.True(t, predictable)
			assert.True(t, testCase.expected.Equal(nextRunAt),
				"expected %s, got %s", testCase.expected, nextRunAt)
		})
	}
}

func TestCronScheduleExpandsAliases(t *testing.T) {
	testCases := []struct {
		alias    string
		expanded string
	}{
		{alias: "@yearly", expanded: "0 0 1 1 *"},
		{alias: "@annually", expanded: "0 0 1 1 *"},
		{alias: "@monthly", expanded: "0 0 1 * *"},
		{alias: "@weekly", expanded: "0 0 * * 0"},
		{alias: "@daily", expanded: "0 0 * * *"},
		{alias: "@midnight", expanded: "0 0 * * *"},
		{alias: "@hourly", expanded: "0 * * * *"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.alias, func(t *testing.T) {
			schedule, err := NewCronSchedule(testCase.alias)
			require.NoError(t, err)

			assert.Equal(t, testCase.expanded, schedule.Expression())
			assert.Equal(t, testCase.alias, schedule.OriginalExpression())
			assert.True(t, schedule.IsPredictable())
		})
	}
}

func TestCronScheduleRebootIsValidButUnpredictable(t *testing.T) {
	schedule, err := NewCronSchedule("@reboot")
	require.NoError(t, err)

	assert.False(t, schedule.IsPredictable())
	assert.Equal(t, "@reboot", schedule.Expression())
	assert.Equal(t, "@reboot", schedule.OriginalExpression())

	nextRunAt, predictable := schedule.NextRunAt(time.Date(2026, 8, 8, 1, 0, 0, 0, taipeiLocation))

	assert.False(t, predictable)
	assert.True(t, nextRunAt.IsZero())
}

func TestCronScheduleNextRunAtKeepsTheCallersLocation(t *testing.T) {
	schedule, err := NewCronSchedule("0 3 * * *")
	require.NoError(t, err)

	from := time.Date(2026, 8, 8, 1, 0, 0, 0, taipeiLocation)
	nextRunAt, predictable := schedule.NextRunAt(from)

	require.True(t, predictable)
	assert.Equal(t, taipeiLocation, nextRunAt.Location())
	assert.Equal(t, "2026-08-08T03:00:00+08:00", nextRunAt.Format(time.RFC3339))

	utcNextRunAt, _ := schedule.NextRunAt(from.UTC())
	assert.Equal(t, time.UTC, utcNextRunAt.Location())
}

func TestCronScheduleExpressionIsTheExpandedFormWhileOriginalIsPreserved(t *testing.T) {
	schedule, err := NewCronSchedule("  0   3  *  *  * ")
	require.NoError(t, err)

	assert.Equal(t, "0 3 * * *", schedule.Expression(), "internal whitespace is normalised")
	assert.Equal(t, "  0   3  *  *  * ", schedule.OriginalExpression(), "original text is preserved verbatim")
}

func TestCronScheduleDescribe(t *testing.T) {
	testCases := []struct {
		expression string
		expected   string
	}{
		{expression: "@reboot", expected: "開機時執行"},
		{expression: "0 0 * * *", expected: "每天 00:00"},
		{expression: "30 3 * * *", expected: "每天 03:30"},
		{expression: "@daily", expected: "每天 00:00"},
		{expression: "0 * * * *", expected: "每小時 00 分"},
		{expression: "15 * * * *", expected: "每小時 15 分"},
		{expression: "*/15 * * * *", expected: "每 15 分鐘"},
		{expression: "*/2 * * * *", expected: "每 2 分鐘"},
		{expression: "0 9 * * 1", expected: "每週一 09:00"},
		{expression: "0 9 * * 0", expected: "每週日 09:00"},
		{expression: "0 0 1 1 *", expected: "0 0 1 1 *"},
		{expression: "0 0 * * 1-5", expected: "0 0 * * 1-5"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.expression, func(t *testing.T) {
			schedule, err := NewCronSchedule(testCase.expression)
			require.NoError(t, err)

			assert.Equal(t, testCase.expected, schedule.Describe())
		})
	}
}
