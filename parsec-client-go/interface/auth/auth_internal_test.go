// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/binary"
	"fmt"
	"os/user"
	"reflect"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("auth", func() {
	Describe("factory", func() {
		var authenticator Authenticator
		Context("Creating no auth authenticator", func() {
			BeforeEach(func() {
				authenticator = NewNoAuthAuthenticator()
			})
			It("Should return *noAuthAuthenticator", func() {
				Expect(reflect.TypeOf(authenticator).String()).To(Equal("*auth.noAuthAuthenticator"))
				Expect(authenticator.GetType()).To(Equal(AuthNoAuth))
			})
			It("Should return an empty auth buffer", func() {
				tok, tokerr := authenticator.NewRequestAuth()
				Expect(tok).NotTo(BeNil())
				Expect(tokerr).NotTo(HaveOccurred())
				Expect(tok.AuthType()).To(Equal(AuthNoAuth))
				buf := tok.Buffer().Bytes()
				Expect(len(buf)).To(Equal(0))
			})
		})
		Context("Creating unix peer authenticator", func() {
			BeforeEach(func() {
				authenticator = NewUnixPeerAuthenticator()
			})
			It("Should return *unixPeerAuthenticator", func() {
				Expect(reflect.TypeOf(authenticator).String()).To(Equal("*auth.unixPeerAuthenticator"))
				Expect(authenticator.GetType()).To(Equal(AuthUnixPeerCredentials))
			})
			It("Should return a 32 bit auth buffer", func() {
				tok, tokerr := authenticator.NewRequestAuth()
				Expect(tok).NotTo(BeNil())
				Expect(tokerr).NotTo(HaveOccurred())
				Expect(tok.AuthType()).To(Equal(AuthUnixPeerCredentials))
				buf := tok.Buffer().Bytes()
				Expect(len(buf)).To(Equal(4))
				currentUser, usererr := user.Current()
				Expect(usererr).NotTo(HaveOccurred())
				var uid uint32
				usererr = binary.Read(tok.Buffer(), binary.LittleEndian, &uid)
				Expect(usererr).NotTo(HaveOccurred())
				Expect(fmt.Sprint(uid)).To(Equal(currentUser.Uid))
			})
		})
		Context("Creating AuthDirect authenticator", func() {
			const appName = "test app name"
			BeforeEach(func() {
				authenticator = NewDirectAuthenticator(appName)
			})
			It("Should return *directAuthenticator", func() {
				Expect(reflect.TypeOf(authenticator).String()).To(Equal("*auth.directAuthenticator"))
				Expect(authenticator.GetType()).To(Equal(AuthDirect))
			})
			It("Should return bytes encoding app name", func() {
				tok, tokerr := authenticator.NewRequestAuth()
				Expect(tok).NotTo(BeNil())
				Expect(tokerr).NotTo(HaveOccurred())
				Expect(tok.AuthType()).To(Equal(AuthDirect))
				buf := tok.Buffer().Bytes()
				Expect(string(buf)).To(Equal(appName))
			})
		})
	})
	Describe("Conversion from uint32", func() {
		Context("For valid types", func() {
			It("Should succeed", func() {
				for a := uint32(0); a <= uint32(AuthJwtSvid); a++ {
					authType, err := NewAuthenticationTypeFromU32(a)
					Expect(err).NotTo(HaveOccurred())
					Expect(authType.IsValid()).To(BeTrue())
				}
			})
		})
		Context("For invalid types", func() {
			It("Should fail", func() {
				for a := uint32(AuthJwtSvid) + 1; a <= uint32(255); a++ {
					authType, err := NewAuthenticationTypeFromU32(a)
					Expect(err).To(HaveOccurred())
					Expect(authType).To(Equal(AuthenticationType(0))) // Returns default value
				}
			})
		})
	})
})
