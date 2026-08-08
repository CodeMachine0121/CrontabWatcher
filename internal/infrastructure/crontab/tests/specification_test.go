package crontab_test

import (
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
