// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package algorithm_test

import (
	"testing"

	"github.com/parallaxsecond/parsec-client-go/parsec/algorithm"
)

func TestAlgorithmSubclasses(t *testing.T) {
	a1 := algorithm.NewCipher(algorithm.CipherModeCFB)
	if a1 == nil {
		t.Fatal("Got nil algorithm")
	}
	if a1.GetAead() != nil || a1.GetAsymmetricSignature() != nil {
		t.Error("Expected only to get non nil subtype for cipher")
	}
	if a1.GetCipher() == nil {
		t.Error("Could not retrieve cipher from generic algorithm")
	}

	a2 := algorithm.NewAead().Aead(algorithm.AeadAlgorithmChacha20Poly1305)
	if a2 == nil {
		t.Fatal("Got nil algorithm")
	}
	if a2.GetCipher() != nil || a2.GetAsymmetricSignature() != nil {
		t.Error("Expected only to get non nil subtype for cipher")
	}
	if ca := a2.GetAead(); ca == nil {
		t.Error("Could not retrieve Aead from generic algorithm")
	} else {
		if cavar := ca.GetAeadShortenedTag(); cavar != nil {
			t.Error("got shortened aead algorithm and was expecting full length")
		} else {
			if cavar := ca.GetAeadDefaultLengthTag(); cavar == nil {
				t.Error("could not get default length aead")
			} else if cavar.AeadAlg != algorithm.AeadAlgorithmChacha20Poly1305 {
				t.Errorf("Expected to have alg type of AeadAlgorithmChacha20Poly1305 but got %v", cavar.AeadAlg)
			}
		}
	}
}
