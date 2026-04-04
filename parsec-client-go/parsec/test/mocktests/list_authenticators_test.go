package mocktests_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/parallaxsecond/parsec-client-go/parsec"
)

var _ = Describe("list authenticators", func() {
	var (
		server *mockServer
		err    error
	)
	BeforeEach(func() {
		server, err = startMockServer()
		Expect(err).NotTo(HaveOccurred())
	})

	Context("With no provider", func() {
		It("Should give us one provider", func() {
			var basicClient *parsec.BasicClient
			basicClient, err = parsec.CreateNakedClient()
			Expect(err).NotTo(HaveOccurred())
			defer basicClient.Close()
			var authInfo []*parsec.AuthenticatorInfo
			authInfo, err = basicClient.ListAuthenticators()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(authInfo)).To(Equal(1))
			Expect(authInfo[0].Description).To(Equal("direct auth"))
		})
	})

	AfterEach(func() {
		server.stop()
	})
})
