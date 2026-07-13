package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHelpDoesNotInitializeExternalServices(t *testing.T) {
	require.NoError(t, run([]string{"--help"}))
}
