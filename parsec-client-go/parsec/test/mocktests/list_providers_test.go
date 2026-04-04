package mocktests_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/parallaxsecond/parsec-client-go/parsec"
)

var _ = Describe("list providers", func() {
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
			var provInfo []*parsec.ProviderInfo
			provInfo, err = basicClient.ListProviders()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(provInfo)).To(Equal(1))
			Expect(provInfo[0].Description).To(Equal("Some empty provider"))
		})
	})

	AfterEach(func() {
		server.stop()
	})
})
