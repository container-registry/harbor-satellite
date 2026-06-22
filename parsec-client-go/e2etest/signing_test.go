// +build end2endtest

// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package e2etest

import (
	"testing"

	parsec "github.com/parallaxsecond/parsec-client-go/parsec"
	"github.com/parallaxsecond/parsec-client-go/parsec/algorithm"
)

func TestSigning(t *testing.T) {

	f := initFixture(t)
	defer f.closeFixture(t)
	f.c.SetImplicitProvider(parsec.ProviderMBed)

	keyname := "sdfasd"
	message := "hello dolly"
	keyatts := parsec.DefaultKeyAttribute().SigningKey()
	keyalg := keyatts.KeyPolicy.KeyAlgorithm.GetAsymmetricSignature()
	if keyalg == nil {
		t.Fatal("Expected to be able to get AsymmetricSignatureAlgorithm back from keyatts, couldnt")
	}
	err := f.c.PsaGenerateKey(keyname, keyatts)
	if err != nil {
		t.Fatal(err)
	}
	f.deferredKeyDestroy(keyname)

	hash, err := f.c.PsaHashCompute([]byte(message), algorithm.HashAlgorithmTypeSHA256)
	if err != nil {
		t.Fatal(err)
	}

	signature, err := f.c.PsaSignHash(keyname, hash, keyalg)
	if err != nil {
		t.Fatal(err)
	}
	err = f.c.PsaVerifyHash(keyname, hash, signature, keyalg)
	if err != nil {
		t.Fatal(err)
	}

	// try verifying signature with hash of another message
	message = "hello world"
	hash, err = f.c.PsaHashCompute([]byte(message), algorithm.HashAlgorithmTypeSHA256)
	if err != nil {
		t.Fatal(err)
	}
	err = f.c.PsaVerifyHash(keyname, hash, signature, keyalg)
	if err == nil {
		t.Fatal(err)

	}

}
