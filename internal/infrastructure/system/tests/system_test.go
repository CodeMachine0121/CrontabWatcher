package system_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/system"
)

func TestIdentifierGeneratorProducesDistinctIdentifiers(t *testing.T) {
	generator := system.NewIdentifierGenerator()

	seenIdentifiers := make(map[string]bool)
	for attempt := 0; attempt < 1000; attempt++ {
		identifier := generator.NewIdentifier()

		require.NotEmpty(t, identifier)
		require.False(t, seenIdentifiers[identifier], "identifiers must not repeat")
		seenIdentifiers[identifier] = true
	}
}

func TestIdentifierGeneratorProducesCrontabSafeIdentifiers(t *testing.T) {
	// 識別碼會被寫進 crontab 的註解與指令參數裡。含空白或 shell 特殊字元就會
	// 把那一行弄壞。
	generator := system.NewIdentifierGenerator()

	for attempt := 0; attempt < 100; attempt++ {
		identifier := generator.NewIdentifier()

		assert.NotContains(t, identifier, " ")
		assert.NotContains(t, identifier, "\n")
		assert.Regexp(t, `^[0-9a-f-]+$`, identifier)
	}
}

func TestClockReturnsTheCurrentTimeInItsLocation(t *testing.T) {
	taipeiLocation, err := time.LoadLocation("Asia/Taipei")
	require.NoError(t, err)

	clock := system.NewClock(taipeiLocation)

	now := clock.Now()

	assert.Equal(t, taipeiLocation, now.Location())
	assert.WithinDuration(t, time.Now(), now, time.Second)
}
