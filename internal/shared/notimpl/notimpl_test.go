package notimpl

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErr_WrapsSentinelAndNamesItem(t *testing.T) {
	err := Err("D1")

	require.True(t, errors.Is(err, ErrNotImplemented))
	require.Contains(t, err.Error(), "D1")
}
