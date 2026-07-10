// +build unsupported_test

// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package e2etest

import (
	"bytes"
	"testing"

	parsec "github.com/parallaxsecond/parsec-client-go/parsec"
	"github.com/parallaxsecond/parsec-client-go/parsec/algorithm"
)

func TestCipherEncrypt(t *testing.T) {

	f := initFixture(t)
	defer f.closeFixture(t)

	const keyname = "cipher_symm_key"
	keyAttrs := &parsec.KeyAttributes{
		KeyBits: 256,
		KeyType: parsec.NewKeyType().Aes(),
		KeyPolicy: &parsec.KeyPolicy{
			KeyAlgorithm: algorithm.NewCipher(algorithm.CipherModeCFB),
			KeyUsageFlags: &parsec.UsageFlags{
				Cache:         false,
				Copy:          false,
				Decrypt:       true,
				Derive:        false,
				Encrypt:       true,
				Export:        false,
				SignHash:      false,
				SignMessage:   false,
				VerifyHash:    false,
				VerifyMessage: false,
			},
		},
	}

	err := f.c.PsaGenerateKey(keyname, keyAttrs)
	if err != nil {
		t.Fatal(err)
	}
	f.deferredKeyDestroy(keyname)

	plaintext := "the quick brown fox"

	ciphertext, err := f.c.PsaCipherEncrypt(keyname, keyAttrs.KeyPolicy.KeyAlgorithm.GetCipher(), []byte(plaintext))

	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(ciphertext, []byte(plaintext)) == 0 {
		t.Fatalf("ciphertext should not be same as plaintext")
	}

	plaintext2, err := f.c.PsaCipherDecrypt(keyname, keyAttrs.KeyPolicy.KeyAlgorithm.GetCipher(), ciphertext)

	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(ciphertext, plaintext2) == 0 {
		t.Fatalf("ciphertext should not be same as plaintext")
	}
	if bytes.Compare([]byte(plaintext), plaintext2) == 0 {
		t.Fatalf("plaintext retrieved from decrypting ciphertext not same as original.  Got %v, expected %v", plaintext2, plaintext)
	}

}
