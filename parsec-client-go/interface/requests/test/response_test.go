// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package requests_test

import (
	"bytes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	// "github.com/parallaxsecond/parsec-client-go/interface/operations/asym_sign"
	"github.com/parallaxsecond/parsec-client-go/interface/operations/ping"
	"github.com/parallaxsecond/parsec-client-go/interface/requests"
)

var expectedPingResp = []byte{
	0x10, 0xa7, 0xc0, 0x5e, // magic
	0x1e, 0x00, // header size
	0x01, 0x00, // verMaj(8), verMin(8)
	0x00, 0x00, // flags(16)
	0x00,                                           // provider
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // session(64)
	0x00,                   // content type
	0x00,                   // accept type
	0x00,                   // auth type
	0x02, 0x00, 0x00, 0x00, // bodylen(32)
	0x00, 0x00, // auth len
	0x01, 0x00, 0x00, 0x00, //   opcode(32)
	0x00, 0x00, // status
	0x00, 0x00, // reserved
	0x08, 0x01} //  body(16)

var mangledPingRespLong = []byte{
	0x10, 0xa7, 0xc0, 0x5e, // magic
	0x1e, 0x00, // header size
	0x01, 0x00, // verMaj(8), verMin(8)
	0x00, 0x00, // flags(16)
	0x00,                                           // provider
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // session(64)
	0x00,                   // content type
	0x00,                   // accept type
	0x00,                   // auth type
	0x08, 0x00, 0x00, 0x00, // bodylen(32)
	0x00, 0x00, // auth len
	0x01, 0x00, 0x00, 0x00, //   opcode(32)
	0x00, 0x00, // status
	0x00, 0x00, // reserved
	0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88} //  body(16)

var mangledPingRespTrunc = []byte{
	0x10, 0xa7, 0xc0, 0x5e, // magic
	0x1e, 0x00, // header size
	0x01, 0x00, // verMaj(8), verMin(8)
	0x00, 0x00, // flags(16)
	0x00,                                           // provider
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // session(64)
	0x00,                   // content type
	0x00,                   // accept type
	0x00,                   // auth type
	0x01, 0x00, 0x00, 0x00, // bodylen(32)
	0x00, 0x00, // auth len
	0x01, 0x00, 0x00, 0x00, //   opcode(32)
	0x00, 0x00, // status
	0x00, 0x00, // reserved
	0x08} //  body(16)

// var expectedSignResp = []byte{
// 	0x10, 0xa7, 0xc0, 0x5e, 0x1e, 0x00, 0x00, 0x00, // magic(32), hdrsize(16), verMaj(8), verMin(8)
// 	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // flags(16), provider(8), session(64 (5/8))
// 	0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x04, 0x00, // session(3/8), contenttype(8), accepttype(8), authtype(8), bodylen(32) (2/4)
// 	0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x0, 0x00, // bodylen(2/4), authlen(16) opcode(32)
// 	0x00, 0x00, 0x00, 0x0a, 0x04, 0x01, 0x02, 0x03, 0x04}
// var expectedSignature = []byte{0x01, 0x02, 0x03, 0x04}

var _ = Describe("response", func() {

	Describe("ping", func() {
		var (
			res *ping.Result
			err error
			// response       *requests.Response
			testbuf        *bytes.Buffer
			expectedOpCode requests.OpCode
		)
		BeforeEach(func() {
			res = &ping.Result{}
			testbuf = bytes.NewBuffer(expectedPingResp)
			expectedOpCode = requests.OpPing
		})
		JustBeforeEach(func() {
			err = requests.ParseResponse(expectedOpCode, testbuf, res)

		})
		Context("good parameters", func() {
			It("should not error", func() {
				Expect(err).NotTo(HaveOccurred())
			})
			It("should have correct version number", func() {
				Expect(res.GetWireProtocolVersionMaj()).To(Equal(uint32(1)))
				Expect(res.GetWireProtocolVersionMin()).To(Equal(uint32(0)))
			})
		})
		Context("nil result", func() {
			BeforeEach(func() {
				res = nil
			})
			It("Should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
		Context("nil buffer", func() {
			BeforeEach(func() {
				res = &ping.Result{}
				testbuf = nil
			})
			It("Should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
		Context("empty buffer", func() {
			BeforeEach(func() {
				res = &ping.Result{}
				testbuf = bytes.NewBuffer([]byte{})
			})
			It("Should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
		Context("zeroed buffer", func() {
			BeforeEach(func() {
				buf := make([]byte, 36)
				for i := 0; i < len(buf); i++ { //nolint:gocritic
					buf[i] = 0x00
				}
				res = &ping.Result{}
				testbuf = bytes.NewBuffer(buf)
			})
			It("Should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
		Context("Short payload buffer", func() {
			BeforeEach(func() {
				buf := make([]byte, len(expectedPingResp)-1)
				copy(buf, expectedPingResp)
				res = &ping.Result{}

				testbuf = bytes.NewBuffer(buf)
			})
			It("Should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
		Context("Empty payload buffer", func() {
			BeforeEach(func() {
				buf := make([]byte, requests.WireHeaderSize)
				copy(buf, expectedPingResp)
				res = &ping.Result{}

				testbuf = bytes.NewBuffer(buf)
			})
			It("Should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
		Context("Incorrect op code", func() {
			BeforeEach(func() {
				expectedOpCode = requests.OpPsaAeadDecrypt
			})
			It("Should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
		Context("Mangled response buffer long", func() {
			BeforeEach(func() {
				testbuf = bytes.NewBuffer(mangledPingRespLong)
			})
			It("Should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
		Context("Mangled response buffer truncated", func() {
			BeforeEach(func() {
				testbuf = bytes.NewBuffer(mangledPingRespTrunc)
			})
			It("Should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("Response Codes", func() {
		It("Should return correct values", func() {
			for i := 0; i < 10000; i++ {
				c := requests.StatusCode(i)
				valid := c.IsValid()
				err := c.ToErr()
				if c == requests.StatusSuccess {
					Expect(valid).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				} else if (i > 0 && i <= 21) || (i >= 1132 && i <= 1152) {
					Expect(valid).To(BeTrue())
					Expect(err).To(HaveOccurred())
				} else {
					Expect(valid).To(BeFalse())
					Expect(err).To(HaveOccurred())
				}
			}
		})
	})

})
