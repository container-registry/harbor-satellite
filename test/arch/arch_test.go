package arch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchLintHasNoExceptions(t *testing.T) {
	cfg, err := os.ReadFile(filepath.Join("..", "..", ".go-arch-lint.yml"))
	require.NoError(t, err)

	var exceptions []string
	for line := range strings.Lines(string(cfg)) {
		if strings.Contains(line, "EXCEPTION") {
			exceptions = append(exceptions, strings.TrimSpace(line))
		}
	}
	require.Empty(t, exceptions, "remove the import that each exception allows, then the exception")
}
