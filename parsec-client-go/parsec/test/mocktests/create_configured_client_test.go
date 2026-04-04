package mocktests_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/parallaxsecond/parsec-client-go/parsec"
)

var _ = Describe("create configured basic client", func() {
	var (
		server *mockServer
		err    error
	)
	BeforeEach(func() {
		server, err = startMockServer()
		Expect(err).NotTo(HaveOccurred())
	})

	It("Should create and configure with no errors", func() {
		var basicClient *parsec.BasicClient
		basicClient, err = parsec.CreateConfiguredClient("admin_priv")
		Expect(err).NotTo(HaveOccurred())
		defer basicClient.Close()
		Expect(basicClient.GetAuthenticatorType()).To(Equal(parsec.AuthDirect))
		Expect(basicClient.GetImplicitProvider()).To(Equal(parsec.ProviderPKCS11))
	})

	AfterEach(func() {
		server.stop()
	})
})
