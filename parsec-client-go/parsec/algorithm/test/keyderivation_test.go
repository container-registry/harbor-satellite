// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package algorithm_test

import (
	"testing"

	"github.com/parallaxsecond/parsec-client-go/parsec/algorithm"
)

func TestKeyDerivationNew(t *testing.T) {
	algorithm.NewKeyDerivation().Hkdf(algorithm.HashAlgorithmTypeMD2) //nolint:staticcheck // this is test code and we're testing this algorithm
}
