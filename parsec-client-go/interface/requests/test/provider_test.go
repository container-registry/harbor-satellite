// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package requests_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/parallaxsecond/parsec-client-go/interface/requests"
)

var _ = Describe("Provider", func() {
	It("Should give correct values", func() {
		for i := 0; i < 255; i++ {
			p := requests.ProviderID(i)
			if p >= requests.ProviderCore && p <= requests.ProviderTrustedService {
				Expect(p.IsValid()).To(BeTrue())
				Expect(p.String()).NotTo(Equal("Unknown"))
			} else {
				Expect(p.IsValid()).To(BeFalse())
				Expect(p.String()).To(Equal("Unknown"))
			}
			if p == requests.ProviderCore {
				Expect(p.HasCrypto()).To(BeFalse())
			} else if p.IsValid() {
				Expect(p.HasCrypto()).To(BeTrue())
			} else {
				Expect(p.HasCrypto()).To(BeFalse())

			}
		}
	})
})
