// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package connection

import (
	"io"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Connection Tests", func() {
	Context("No environment variable", func() {
		BeforeEach(func() {
			os.Setenv("PARSEC_SERVICE_ENDPOINT", "")
		})
		AfterEach(func() {
			os.Setenv("PARSEC_SERVICE_ENDPOINT", "")
		})
		It("Should have default address", func() {
			c, err := NewDefaultConnection()
			Expect(err).NotTo(HaveOccurred())
			Expect(c).NotTo(BeNil())
			uc, ok := c.(*unixConnection)
			Expect(ok).To(BeTrue())
			Expect(uc).NotTo(BeNil())
			Expect(uc.path).To(Equal("/run/parsec/parsec.sock"))
		})
	})
	Context("Set environment variable", func() {
		var sockPath string

		BeforeEach(func() {
			file, err := ioutil.TempFile("/tmp", "socktest")
			Expect(err).NotTo(HaveOccurred())
			sockPath = file.Name()
			os.Setenv("PARSEC_SERVICE_ENDPOINT", "unix:"+sockPath)
			err = os.RemoveAll(file.Name())
			Expect(err).NotTo(HaveOccurred())
			l, err := net.Listen("unix", file.Name())
			Expect(err).NotTo(HaveOccurred())
			go func() {
				defer l.Close()
				unixL, ok := l.(*net.UnixListener)
				Expect(ok).To(BeTrue())
				err = unixL.SetDeadline(time.Now().Add(time.Second * 2))
				Expect(err).NotTo(HaveOccurred())
				conn, err := l.Accept()
				Expect(err).NotTo(HaveOccurred())
				defer conn.Close()
				_, err = io.Copy(conn, conn)
				Expect(err).NotTo(HaveOccurred())
			}()
		})
		AfterEach(func() {
			os.Setenv("PARSEC_SERVICE_ENDPOINT", "")
		})
		It("Should have the configured address and be usable", func() {
			c, err := NewDefaultConnection()
			Expect(err).NotTo(HaveOccurred())
			Expect(c).NotTo(BeNil())
			uc, ok := c.(*unixConnection)
			Expect(ok).To(BeTrue())
			Expect(uc).NotTo(BeNil())
			Expect(uc.path).To(Equal(sockPath))

			err = uc.Open()
			Expect(err).NotTo(HaveOccurred())
			n, err := uc.Write([]byte("hello"))
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(5))
			buf := make([]byte, 10)
			n, err = uc.Read(buf)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(5))
			Expect(string(buf[:n])).To(Equal("hello"))
			err = uc.Close()
			Expect(err).NotTo(HaveOccurred())
		})
		It("Should not allow use before open", func() {
			c, err := NewDefaultConnection()
			Expect(err).NotTo(HaveOccurred())
			Expect(c).NotTo(BeNil())
			uc, ok := c.(*unixConnection)
			Expect(ok).To(BeTrue())
			Expect(uc).NotTo(BeNil())
			Expect(uc.path).To(Equal(sockPath))

			buf := make([]byte, 10)

			_, err = uc.Write([]byte("hello"))
			Expect(err).To(HaveOccurred())
			_, err = uc.Read(buf)
			Expect(err).To(HaveOccurred())
			err = uc.Close()
			// We allow closed connections to be closed
			Expect(err).NotTo(HaveOccurred())

			err = uc.Open()
			Expect(err).NotTo(HaveOccurred())
			n, err := uc.Write([]byte("hello"))
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(5))
			n, err = uc.Read(buf)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(5))
			Expect(string(buf[:n])).To(Equal("hello"))
			err = uc.Close()
			Expect(err).NotTo(HaveOccurred())

			_, err = uc.Write([]byte("hello"))
			Expect(err).To(HaveOccurred())
			_, err = uc.Read(buf)
			Expect(err).To(HaveOccurred())
			err = uc.Close()
			Expect(err).NotTo(HaveOccurred())

		})

	})
	Context("Invalid endpoint scheme", func() {
		BeforeEach(func() {
			os.Setenv("PARSEC_SERVICE_ENDPOINT", "http://google.com")
		})
		AfterEach(func() {
			os.Setenv("PARSEC_SERVICE_ENDPOINT", "")
		})
		It("Should fail on on create", func() {
			_, err := NewDefaultConnection()
			Expect(err).To(HaveOccurred())
		})

	})
})

func TestRequests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "connection package external test suite")
}
