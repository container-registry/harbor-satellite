// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0
package test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/parallaxsecond/parsec-client-go/interface/connection"
	"github.com/parallaxsecond/parsec-client-go/parsec"
)

// loadTestData loads a list of test case json files and parses them into a map of TestCase objects, keyed by the testcase name.
func loadTestData(fileNames []string) map[string]testCase {
	testMap := make(map[string]testCase)

	for _, fileName := range fileNames {
		jsonfile, err := os.Open(fileName)
		Expect(err).NotTo(HaveOccurred())
		byteValue, err := io.ReadAll(jsonfile)
		Expect(err).NotTo(HaveOccurred())
		var testSuite struct {
			Tests []testCase `json:"tests"`
		}

		err = json.Unmarshal(byteValue, &testSuite)
		fmt.Println(err)
		Expect(err).NotTo(HaveOccurred())

		for _, tc := range testSuite.Tests {
			testMap[tc.Name] = tc
		}
	}

	return testMap
}

func TestRequests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "requests package internal suite")
}

var _ = Describe("Basic Client provider behaviour", func() {
	testCases := loadTestData([]string{"list_providers.json", "list_authenticators.json"})
	var connection connection.Connection
	BeforeEach(func() {
		connection = newMockConnectionFromTestCase([]testCase{testCases["auth_direct"], testCases["provider_mbed"]})
	})
	Context("Default", func() {
		It("should have mbed as default", func() {
			bc, err := parsec.CreateConfiguredClient(parsec.DirectAuthConfigData("testapp").Connection(connection))
			Expect(err).NotTo(HaveOccurred())
			Expect(bc).NotTo(BeNil())

			Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderMBed))
		})
	})
	Context("Set Implicit to Tpm", func() {
		It("Should allow us to change provider", func() {
			bc, err := parsec.CreateConfiguredClient(parsec.DirectAuthConfigData("testapp").Connection(connection))
			Expect(err).NotTo(HaveOccurred())
			Expect(bc).NotTo(BeNil())
			bc.SetImplicitProvider(parsec.ProviderTPM)
			Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderTPM))
		})
	})
	Describe("Auto selection of authenticator", func() {
		var tc []testCase
		JustBeforeEach(func() {
			connection = newMockConnectionFromTestCase(tc)
		})
		Context("service supports only default", func() {
			BeforeEach(func() {
				tc = []testCase{testCases["auth_direct"], testCases["provider_mbed"]}
			})
			It("Should return direct if we have direct auth data", func() {
				bc, err := parsec.CreateConfiguredClient(parsec.DirectAuthConfigData("testapp").Connection(connection))
				Expect(err).NotTo(HaveOccurred())
				Expect(bc).NotTo(BeNil())
				Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderMBed))
				Expect(bc.GetAuthenticatorType()).To(Equal(parsec.AuthDirect))
			})
			It("Should return none if we have no direct auth data", func() {
				bc, err := parsec.CreateConfiguredClient(parsec.NewClientConfig().Connection(connection))
				Expect(err).NotTo(HaveOccurred())
				Expect(bc).NotTo(BeNil())
				Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderMBed))
				Expect(bc.GetAuthenticatorType()).To(Equal(parsec.AuthNoAuth))
			})
		})
		Context("service supports direct, unix", func() {
			BeforeEach(func() {
				tc = []testCase{testCases["auth_direct,unix"], testCases["provider_mbed"]}
			})
			It("Should return direct if we have direct auth data", func() {
				bc, err := parsec.CreateConfiguredClient(parsec.DirectAuthConfigData("testapp").Connection(connection))
				Expect(err).NotTo(HaveOccurred())
				Expect(bc).NotTo(BeNil())
				Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderMBed))
				Expect(bc.GetAuthenticatorType()).To(Equal(parsec.AuthDirect))
			})
			It("Should return unix if we have no direct auth data", func() {
				bc, err := parsec.CreateConfiguredClient(parsec.NewClientConfig().Connection(connection))
				Expect(err).NotTo(HaveOccurred())
				Expect(bc).NotTo(BeNil())
				Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderMBed))
				Expect(bc.GetAuthenticatorType()).To(Equal(parsec.AuthUnixPeerCredentials))
			})
		})
		Context("service supports unix,direct", func() {
			BeforeEach(func() {
				tc = []testCase{testCases["auth_unix,direct"], testCases["provider_mbed"]}
			})
			It("Should return unix even if we have direct auth data", func() {
				bc, err := parsec.CreateConfiguredClient(parsec.DirectAuthConfigData("testapp").Connection(connection))
				Expect(err).NotTo(HaveOccurred())
				Expect(bc).NotTo(BeNil())
				Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderMBed))
				Expect(bc.GetAuthenticatorType()).To(Equal(parsec.AuthUnixPeerCredentials))
			})
			It("Should return unix if we have no direct auth data", func() {
				bc, err := parsec.CreateConfiguredClient(parsec.NewClientConfig().Connection(connection))
				Expect(err).NotTo(HaveOccurred())
				Expect(bc).NotTo(BeNil())
				Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderMBed))
				Expect(bc.GetAuthenticatorType()).To(Equal(parsec.AuthUnixPeerCredentials))
			})
		})
		Context("service supports tpm,mbed providers", func() {
			BeforeEach(func() {
				tc = []testCase{testCases["auth_direct"], testCases["provider_tpm,mbed"]}
			})
			It("Should return tpm provider", func() {
				bc, err := parsec.CreateConfiguredClient(parsec.DirectAuthConfigData("testapp").Connection(connection))
				Expect(err).NotTo(HaveOccurred())
				Expect(bc).NotTo(BeNil())
				Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderTPM))
				Expect(bc.GetAuthenticatorType()).To(Equal(parsec.AuthDirect))
			})
		})
	})
	Describe("Set authenticator and provider in client config", func() {
		Context("set provider to tpm and authentictor to unix", func() {
			It("Should return configured provider and authenticator, and not call parsec service", func() {
				config := parsec.NewClientConfig().
					Provider(parsec.ProviderTPM).
					Authenticator(parsec.NewUnixPeerAuthenticator()).
					Connection(newNoopConnection()) // This connection will fail the test if it is called
				bc, err := parsec.CreateConfiguredClient(config)
				Expect(err).NotTo(HaveOccurred())
				Expect(bc).NotTo(BeNil())
				Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderTPM))
				Expect(bc.GetAuthenticatorType()).To(Equal(parsec.AuthUnixPeerCredentials))
			})
		})
	})
	Describe("Test naked creation", func() {
		It("Should be configured with core provider and no auth authenticator", func() {
			bc, err := parsec.CreateNakedClient()
			Expect(err).NotTo(HaveOccurred())
			Expect(bc).NotTo(BeNil())
			Expect(bc.GetImplicitProvider()).To(Equal(parsec.ProviderCore))
			Expect(bc.GetAuthenticatorType()).To(Equal(parsec.AuthNoAuth))
		})
	})
})
