package mocktests_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/parallaxsecond/parsec-client-go/parsec"
)

var _ = Describe("list clients", func() {
	var (
		server *mockServer
		err    error
	)
	BeforeEach(func() {
		server, err = startMockServer()
		Expect(err).NotTo(HaveOccurred())
	})

	Context("With direct auth admin_priv", func() {
		It("Should give us two clients", func() {
			var basicClient *parsec.BasicClient
			basicClient, err = parsec.CreateConfiguredClient("admin_priv")
			Expect(err).NotTo(HaveOccurred())
			defer basicClient.Close()
			var clients []string
			clients, err = basicClient.ListClients()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(clients)).To(Equal(2))
			Expect(clients).To(Equal([]string{"jim", "bob"}))
		})
	})
	Context("With direct auth no_admin", func() {
		It("Should error when listing clients", func() {
			var basicClient *parsec.BasicClient
			basicClient, err = parsec.CreateConfiguredClient("no_admin")
			Expect(err).NotTo(HaveOccurred())
			defer basicClient.Close()
			var clients []string
			clients, err = basicClient.ListClients()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("the operation requires admin privilege"))
			Expect(clients).To(BeNil())
		})

	})

	AfterEach(func() {
		server.stop()
	})
})
