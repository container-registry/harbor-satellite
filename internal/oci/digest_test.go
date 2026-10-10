package oci

import (
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestVerifyDigestSuccess(t *testing.T) {
	content := []byte("hello world")
	expected := digest.FromBytes(content).String()

	err := VerifyDigest(content, expected)
	require.NoError(t, err)
}

func TestVerifyDigestMismatch(t *testing.T) {
	content := []byte("hello world")
	wrongExpected := digest.FromBytes([]byte("wrong content")).String()

	err := VerifyDigest(content, wrongExpected)
	require.Error(t, err)
	require.Contains(t, err.Error(), "digest mismatch")
}

func TestVerifyDigestInvalidDigest(t *testing.T) {
	content := []byte("hello world")
	invalidExpected := "not-a-valid-digest"

	err := VerifyDigest(content, invalidExpected)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse expected digest")
}

func TestVerifyDigestEmptyContent(t *testing.T) {
	content := []byte{}
	expected := digest.FromBytes(content).String()

	err := VerifyDigest(content, expected)
	require.NoError(t, err)
}
