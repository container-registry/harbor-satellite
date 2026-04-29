// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package requests_test

import (
	"bytes"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/parallaxsecond/parsec-client-go/interface/auth"
	"github.com/parallaxsecond/parsec-client-go/interface/operations/ping"
	"github.com/parallaxsecond/parsec-client-go/interface/requests"
)

type failingAuthenticator struct {
	returnTok bool
}

func (a *failingAuthenticator) GetType() auth.AuthenticationType {
	return auth.AuthDirect
}
func (a *failingAuthenticator) NewRequestAuth() (auth.RequestAuthToken, error) {
	if a.returnTok {
		return nilBufRequestAuthToken{}, nil
	}
	return nilBufRequestAuthToken{}, fmt.Errorf("deliberate error")
}

type nilBufRequestAuthToken struct{}

func (t nilBufRequestAuthToken) Buffer() *bytes.Buffer {
	return nil
}
func (t nilBufRequestAuthToken) AuthType() auth.AuthenticationType {
	return auth.AuthNoAuth
}

var expectedPingReq = []byte{
	0x10, 0xa7, 0xc0, 0x5e, 0x1e, 0x00, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00}

// var expectedSignReq = []byte{
// 	0x10, 0xa7, 0xc0, 0x5e, 0x16, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x36, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
// 	0x0a, 0x08, 0x74, 0x65, 0x73, 0x74, 0x5f, 0x6b, 0x65, 0x79, 0x10, 0x01, 0x1a, 0x28, 0x74, 0x65, 0x73, 0x74, 0x5f, 0x6d, 0x73, 0x67, 0xe3, 0xb0, 0xc4, 0x42, 0x98, 0xfc, 0x1c, 0x14, 0x9a, 0xfb, 0xf4, 0xc8, 0x99, 0x6f, 0xb9, 0x24, 0x27, 0xae, 0x41, 0xe4, 0x64, 0x9b, 0x93, 0x4c, 0xa4, 0x95, 0x99, 0x1b, 0x78, 0x52, 0xb8, 0x55}

var _ = Describe("request", func() {
	Describe("ping", func() {
		var (
			authenticator auth.Authenticator
			err, errpack  error
			req           *requests.Request
			packBuf       *bytes.Buffer
		)
		BeforeEach(func() {
			authenticator = auth.NewNoAuthAuthenticator()
		})
		JustBeforeEach(func() {
			p := &ping.Result{}
			req, err = requests.NewRequest(requests.OpPing, p, authenticator, requests.ProviderCore)
			if err == nil && req != nil {
				packBuf, errpack = req.Pack()
			}

		})
		Context("Parameters all correct", func() {
			It("should not error", func() {
				Expect(err).NotTo(HaveOccurred())
			})
			It("Should not return nil request", func() {
				Expect(req).NotTo(BeNil())
			})
			It("Should not give a pack error", func() {
				Expect(errpack).NotTo(HaveOccurred())
			})
			It("Should give a non nil buffer", func() {
				Expect(packBuf).NotTo(BeNil())
			})
			It("Should have buffer with expected contents", func() {
				Expect(packBuf.Bytes()).To(Equal([]byte(expectedPingReq))) //nolint:unconvert // required cast to ensure slice not array
			})

		})
		Context("Authenticator returns error instead of token", func() {
			BeforeEach(func() {
				authenticator = &failingAuthenticator{returnTok: false}
			})
			It("should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
		Context("Authenticator returns token with nil buffer", func() {
			It("should panic", func() {
				Expect(func() {
					authenticator = &failingAuthenticator{returnTok: true}
					p := &ping.Result{}
					_, _ = requests.NewRequest(requests.OpPing, p, authenticator, requests.ProviderCore)
				}).To(Panic())
			})
		})
	})
})
