// +build end2endtest

// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package e2etest

import (
	"testing"

	parsec "github.com/parallaxsecond/parsec-client-go/parsec"
)

func TestInitialiseClient(t *testing.T) {
	c, err := parsec.CreateConfiguredClient("ci test app")
	if err != nil {
		t.Fatal(err)
		return
	}

	err = c.Close()
	if err != nil {
		t.Fatal(err)
		return
	}
}

func TestPing(t *testing.T) {
	f := initFixture(t)
	defer f.closeFixture(t)

	majver, minver, err := f.c.Ping()
	if err != nil {
		t.Fatalf("got an error from ping: %v", err)
	}
	if majver != 1 && minver != 0 {
		t.Fatalf("Expected version 1.0, got %v,%v", majver, minver)
	}
}
func TestManageKeys(t *testing.T) {
	f := initFixture(t)
	defer f.closeFixture(t)

	if f.c.GetImplicitProvider() != parsec.ProviderPKCS11 {
		t.Fatalf("expected to have pkcs11 provider, got %v", f.c.GetImplicitProvider())
	}
	// Create a new key
	const keyName = "testManageKeys"
	kattrs := parsec.DefaultKeyAttribute().SigningKey()
	if kattrs == nil {
		t.Fatal("got nil key attributes")
	}
	err := f.c.PsaGenerateKey(keyName, kattrs)
	if err != nil {
		t.Fatal(err)
	}

	f.deferredKeyDestroy(keyName)

	// Created key, see if we can see it with list keys
	keyList, err := f.c.ListKeys()
	if err != nil {
		t.Fatal(err)
	}
	if keyList == nil {
		t.Fatal("returned nil key list")
	}
	var foundKey *parsec.KeyInfo
	for _, kinfo := range keyList {
		if kinfo.Name == keyName {
			foundKey = kinfo
		}
	}
	if foundKey == nil {
		t.Errorf("Could not find created key in list_keys result %v", keyName)
	}
	if !t.Failed() {
		// TODO really need to fix types in return structure
		if foundKey.ProviderID != parsec.ProviderPKCS11 {
			t.Errorf("Expected key to have PKCS11 provider, instead found %v", foundKey.ProviderID)
		}
	}
	// And destroy the key - we want to test it gets destroyed without error here
	err = f.c.PsaDestroyKey(keyName)
	if err != nil {
		t.Fatal(err)
	}

}
