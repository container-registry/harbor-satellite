//go:build end2endtest
// +build end2endtest

// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0
package e2etest

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	parsec "github.com/parallaxsecond/parsec-client-go/parsec"
)

var _ = Describe("admin mode client management operations", func() {
	var (
		basicClient *parsec.BasicClient
		err         error
	)
	BeforeEach(func() {
		basicClient, err = parsec.CreateConfiguredClient(
			parsec.NewClientConfig().Authenticator(parsec.NewUnixPeerAuthenticator()),
		)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("delete keys by deleting client", func() {
		It("should succeed if we delete client and we should have no keys remaining", func() {
			// As we're root user, should be able to list clients
			clients, err := basicClient.ListClients()
			Expect(err).NotTo(HaveOccurred())
			// We'll delete any clients (should succeed if there are any)
			for _, client := range clients {
				err = basicClient.DeleteClient(client)
				Expect(err).NotTo(HaveOccurred())
			}

			// Check there are no keys present
			keyinfarr, err := basicClient.ListKeys()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(keyinfarr)).To(Equal(0))

			// Create a key
			err = basicClient.PsaGenerateKey("key1", parsec.DefaultKeyAttribute().SigningKey())
			Expect(err).NotTo(HaveOccurred())

			// Check it is there
			keyinfarr, err = basicClient.ListKeys()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(keyinfarr)).To(Equal(1))

			// Check we have one client
			clients, err = basicClient.ListClients()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(clients)).To(Equal(1))

			// Delete the client
			err = basicClient.DeleteClient(clients[0])
			Expect(err).NotTo(HaveOccurred())

			// Check we hae no keys left
			keyinfarr, err = basicClient.ListKeys()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(keyinfarr)).To(Equal(0))
		})

	})
	AfterEach(func() {
		if basicClient != nil {
			basicClient.Close()
		}
	})
})
