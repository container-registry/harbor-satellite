// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package requests

import (
	"bytes"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var goodHeader = []byte{
	0x10, 0xa7, 0xc0, 0x5e, // Magic number
	0x1e, 0x00, // header length
	0x01, 0x00, // Major/minor version numbers
	0x00, 0x00, // flags
	0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Session handle
	0x00,                   // Content type
	0x01,                   // Accept type
	0x00,                   // Auth type
	0x00, 0x00, 0x00, 0x00, // Content length
	0x00, 0x00, // Auth length
	0x00, 0x00, 0x00, 0x00, // Op code
	0x00, 0x00, // Status
	0x00, 0x00, // reserved
}

var blankHeader = []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
var _ = Describe("wireHeader", func() {

	Describe("nil buffers", func() {
		Context("pack", func() {
			It("Should panic as internal interface should not be passed nil", func() {
				Expect(func() {
					header := wireHeader{}
					header.pack(nil) //nolint:errcheck //expecting panic here, so don't bother checking error
				}).To(Panic())
			})

		})
		Context("parse", func() {
			It("Should panic as internal interface should not be passed nil", func() {
				Expect(func() {
					parseWireHeaderFromBuf(nil) //nolint:errcheck //expecting panic here, so don't bother checking error
				}).To(Panic())
			})

		})
	})
	Describe("Testing pack", func() {
		Context("Pack a default initialised wireHeader", func() {
			var (
				err    error
				buf    *bytes.Buffer
				header *wireHeader
			)
			BeforeEach(func() {
				header = &wireHeader{}
				buf = bytes.NewBuffer([]byte{})
				err = header.pack(buf)
			})
			Context("pack", func() {

				It("should error as fields not correct", func() {
					Expect(err).To(HaveOccurred())
				})
				It("Should have length zero", func() {
					Expect(buf.Len()).To(Equal(0))
				})
			})
		})
	})
	Describe("Testing parse", func() {
		var (
			err    error
			buf    *bytes.Buffer
			header *wireHeader
		)
		BeforeEach(func() {
			buf = bytes.NewBuffer(goodHeader)
		})
		JustBeforeEach(func() {
			header, err = parseWireHeaderFromBuf(buf)
		})
		Context("parse good header", func() {
			It("should return non nil header", func() {
				Expect(header).NotTo(BeNil())
			})
			It("should not error", func() {
				Expect(err).NotTo(HaveOccurred())
			})
			It("Should have correct magic number", func() {
				Expect(header.magicNumber).To(Equal(magicNumber))
			})
			It("Should have header length 30", func() {
				Expect(header.hdrSize).Should(Equal(uint16(30)))
			})
		})
		Context("blank header", func() {
			BeforeEach(func() {
				buf = bytes.NewBuffer(blankHeader)
			})
			It("should return nil header", func() {
				Expect(header).To(BeNil())
			})
			It("Should error", func() {
				Expect(err).To(HaveOccurred())
			})
		})
		Describe("Check field values", func() {
			BeforeEach(func() {
				hdr := make([]byte, len(goodHeader))
				copy(hdr, goodHeader)
				buf = bytes.NewBuffer(hdr)

			})
			Describe("header size", func() {
				Context("valid magic number but zero length", func() {
					BeforeEach(func() {
						buf.Bytes()[4] = 0x00
						buf.Bytes()[5] = 0x00
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
				Context("valid magic number but length (wireHeaderSizeValue-1)", func() {
					BeforeEach(func() {
						buf.Bytes()[4] = uint8(wireHeaderSizeValue - 1)
						buf.Bytes()[5] = 0x00
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
				Context("valid magic number but length (wireHeaderSizeValue+1)", func() {
					BeforeEach(func() {
						buf.Bytes()[4] = uint8(wireHeaderSizeValue + 1)
						buf.Bytes()[5] = 0x00
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})

			})
			Describe("version numbers", func() {
				Context("zero major version number", func() {
					BeforeEach(func() {
						buf.Bytes()[6] = 0x00
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
				Context("major version number > 0x01", func() {
					BeforeEach(func() {
						buf.Bytes()[6] = 0x02
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
				Context("minor version number > 0x00", func() {
					BeforeEach(func() {
						buf.Bytes()[7] = 0x01
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})

			})
			Describe("flags", func() {
				Context("first byte wrong", func() {
					BeforeEach(func() {
						buf.Bytes()[8] = 0x01
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
				Context("second byte wrong", func() {
					BeforeEach(func() {
						buf.Bytes()[9] = 0x01
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})

				})
			})
			Describe("provider id", func() {
				Context("one more than max providers", func() {
					BeforeEach(func() {
						buf.Bytes()[10] = 0x05
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})

				})
			})
			Describe("content type", func() {
				Context("zero content type", func() {
					BeforeEach(func() {
						buf.Bytes()[19] = 0x00
					})
					It("should return non nil header", func() {
						Expect(header).NotTo(BeNil())
					})
					It("Should error", func() {
						Expect(err).NotTo(HaveOccurred())
					})
				})

				Context("content type > 0x00", func() {
					BeforeEach(func() {
						buf.Bytes()[19] = 0x01
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})

				})
			})
			Describe("accept type", func() {
				Context("any value should succeed (request only type so must ignore)", func() {
					for a := 0; a < 256; a++ {
						aVal := a
						BeforeEach(func() {
							buf.Bytes()[20] = uint8(aVal)
						})
						It(fmt.Sprintf("Should accept a value of %v", aVal), func() {
							hdr := make([]byte, len(goodHeader))
							copy(hdr, goodHeader)
							buf = bytes.NewBuffer(hdr)
							buf.Bytes()[20] = uint8(aVal)
							header, err = parseWireHeaderFromBuf(buf)
							Expect(header).NotTo(BeNil())
							Expect(err).NotTo(HaveOccurred())

						})
					}
				})
			})
			Describe("authentication type", func() {
				Context("auth type > 0x05", func() {
					BeforeEach(func() {
						buf.Bytes()[21] = 0x06
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
			})

			Describe("opcode", func() {
				Context("op code > OpDeleteClient", func() {
					BeforeEach(func() {
						buf.Bytes()[28] = 0x1D // just need to set lsb
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
			})

			Describe("status code", func() {
				Context("status code > max system call", func() {
					BeforeEach(func() {
						buf.Bytes()[32] = 22 // just need to set lsb
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
				Context("status code < min psa call", func() {
					BeforeEach(func() {
						// set value of 1131
						buf.Bytes()[32] = 0x6b
						buf.Bytes()[33] = 0x04
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
				Context("status code > max psa call", func() {
					BeforeEach(func() {
						// set value of 1152
						buf.Bytes()[32] = 0x81
						buf.Bytes()[33] = 0x04
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
			})
			Describe("reserved", func() {
				Context("non zero reserved1", func() {
					BeforeEach(func() {
						// set value of 1152
						buf.Bytes()[34] = 0x01
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
				Context("non zero reserved2", func() {
					BeforeEach(func() {
						// set value of 1152
						buf.Bytes()[35] = 0x01
					})
					It("should return nil header", func() {
						Expect(header).To(BeNil())
					})
					It("Should error", func() {
						Expect(err).To(HaveOccurred())
					})
				})
			})

		})

	})

})
