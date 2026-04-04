// +build end2endtest

// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package e2etest

import (
	"bytes"
	"testing"

	parsec "github.com/parallaxsecond/parsec-client-go/parsec"
	"github.com/parallaxsecond/parsec-client-go/parsec/algorithm"
)

func TestAead(t *testing.T) {

	f := initFixture(t)
	defer f.closeFixture(t)
	f.c.SetImplicitProvider(parsec.ProviderMBed)

	const keyname = "cipher_symm_key"
	keyAttrs := &parsec.KeyAttributes{
		KeyBits: 256,
		KeyType: parsec.NewKeyType().Aes(),
		KeyPolicy: &parsec.KeyPolicy{
			KeyAlgorithm: algorithm.NewAead().Aead(algorithm.AeadAlgorithmGCM),
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
	nonce := []byte("nonce")
	additionalData := []byte("extradata")
	ciphertext, err := f.c.PsaAeadEncrypt(keyname, keyAttrs.KeyPolicy.KeyAlgorithm.GetAead(), nonce, additionalData, []byte(plaintext))

	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(ciphertext, []byte(plaintext)) == 0 {
		t.Fatalf("ciphertext should not be same as plaintext")
	}

	plaintext2, err := f.c.PsaAeadDecrypt(keyname, keyAttrs.KeyPolicy.KeyAlgorithm.GetAead(), nonce, additionalData, ciphertext)

	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ciphertext, plaintext2) {
		t.Fatalf("ciphertext should not be same as plaintext")
	}
	if !bytes.Equal([]byte(plaintext), plaintext2) {
		t.Fatalf("plaintext retrieved from decrypting ciphertext not same as original.  Got %v, expected %v", plaintext2, []byte(plaintext))
	}

	// test with changed nonce, should fail
	plaintext2, err = f.c.PsaAeadDecrypt(keyname, keyAttrs.KeyPolicy.KeyAlgorithm.GetAead(), []byte("newnonce"), additionalData, ciphertext)
	if err == nil {
		t.Fatal("Expected decrypt to fail if using wrong nonce")
	}

	// test with changed additional data, should fail
	plaintext2, err = f.c.PsaAeadDecrypt(keyname, keyAttrs.KeyPolicy.KeyAlgorithm.GetAead(), nonce, []byte("fakedata"), ciphertext)
	if err == nil {
		t.Fatal("Expected decrypt to fail if using wrong additional data")
	}

}
