// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package algorithm_test

import (
	"testing"

	"github.com/parallaxsecond/parsec-client-go/parsec/algorithm"
)

func TestHash(t *testing.T) {
	// Make sure that HashAlgorithm can be assigned to Algorithm
	type algstruct struct {
		alg *algorithm.Algorithm
	}

	a := &algstruct{
		alg: algorithm.NewHashAlgorithm(algorithm.HashAlgorithmTypeMD5), //nolint:staticcheck // this is test code and we're testing this algorithm
	}
	if a.alg == nil {
		t.Fatal("could not construct algorithm structure")
	}

	if ha := a.alg.GetHash(); ha != nil {
		if ha.HashAlg != algorithm.HashAlgorithmTypeMD5 { //nolint:staticcheck // this is test code and we're testing this algorithm
			t.Fatalf("Expected alg to be of type HashAlgorithmTypeMD5, was actually %v", ha.HashAlg)
		}
	} else {
		t.Fatal("Expected alg element to be Hash Algorithm, it wasn't")
	}
}

func TestHashString(t *testing.T) {
	if algorithm.HashAlgorithmTypeMD2.String() != "MD2" { //nolint:staticcheck // this is test code and we're testing this algorithm
		t.Fatal("Incorrect string value from hash enum")
	}
	if algorithm.NewHashAlgorithm(algorithm.HashAlgorithmTypeSHA256).GetHash().String() != "SHA_256" {
		t.Fatal("Incorrect string value for hash algorithm")
	}
}
