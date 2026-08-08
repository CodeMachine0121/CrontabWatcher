package entity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func readFixture(t *testing.T, fixtureName string) string {
	t.Helper()

	contentBytes, err := os.ReadFile(filepath.Join("testdata", fixtureName))
	require.NoError(t, err)

	return string(contentBytes)
}
