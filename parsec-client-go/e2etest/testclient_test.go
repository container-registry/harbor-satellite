// +build end2endtest

// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package e2etest

import (
	"testing"

	parsec "github.com/parallaxsecond/parsec-client-go/parsec"
)

type testClient struct {
	c           *parsec.BasicClient
	createdKeys []string
}

func initFixture(t *testing.T) *testClient {
	c, err := parsec.CreateConfiguredClient("ci-test-client")
	if err != nil {
		t.Fatal(err)
		return nil
	}
	c.SetImplicitProvider(parsec.ProviderPKCS11)
	return &testClient{c: c, createdKeys: make([]string, 0, 0)}
}

func (f *testClient) closeFixture(t *testing.T) {
	// Destroy any keys (may have gone anyway so ignore errors)
	for _, k := range f.createdKeys {
		_ = f.c.PsaDestroyKey(k)
	}
	err := f.c.Close()
	if err != nil {
		t.Fatal(err)
		return
	}

}

func (f *testClient) deferredKeyDestroy(keyName string) {
	f.createdKeys = append(f.createdKeys, keyName)
}
