package mocktests_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/parallaxsecond/parsec-client-go/interface/requests"
	"github.com/parallaxsecond/parsec-client-go/parsec"
)

var _ = Describe("delete client", func() {
	var (
		server *mockServer
		err    error
	)
	BeforeEach(func() {
		server, err = startMockServer()
		Expect(err).NotTo(HaveOccurred())
	})

	Context("With direct auth admin_priv", func() {
		Context("delete exiting client", func() {
			It("Should should give us no error", func() {
				var basicClient *parsec.BasicClient
				basicClient, err = parsec.CreateConfiguredClient("admin_priv")
				Expect(err).NotTo(HaveOccurred())
				defer basicClient.Close()
				err = basicClient.DeleteClient("existing client")
				Expect(err).NotTo(HaveOccurred())
			})
		})
		Context("with non existing client", func() {
			It("Should should error", func() {
				var basicClient *parsec.BasicClient
				basicClient, err = parsec.CreateConfiguredClient("admin_priv")
				Expect(err).NotTo(HaveOccurred())
				defer basicClient.Close()
				err = basicClient.DeleteClient("not exist")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(requests.StatusPsaErrorDoesNotExist.ToErr().Error()))
			})

		})
	})
	Context("With direct auth no_admin", func() {
		It("Should error when listing clients", func() {
			var basicClient *parsec.BasicClient
			basicClient, err = parsec.CreateConfiguredClient("no_admin")
			Expect(err).NotTo(HaveOccurred())
			defer basicClient.Close()
			err = basicClient.DeleteClient("client exists")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal(requests.StatusAdminOperation.ToErr().Error()))
		})

	})

	AfterEach(func() {
		server.stop()
	})
})
