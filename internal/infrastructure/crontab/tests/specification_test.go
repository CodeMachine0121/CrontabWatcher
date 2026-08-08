package crontab_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

const wrapperBinaryPath = "/app/cronwatch"

func mustBuildSpecification(t *testing.T, scheduleExpression string, command string) entity.ManagedJobSpecification {
	t.Helper()

	specification, err := entity.NewManagedJobSpecification(scheduleExpression, command, "", true, wrapperBinaryPath)
	require.NoError(t, err)

	return specification
}

func readFixtureFile(t *testing.T, fixtureName string) string {
	t.Helper()

	contentBytes, err := os.ReadFile(filepath.Join("testdata", fixtureName))
	require.NoError(t, err)

	return string(contentBytes)
}
