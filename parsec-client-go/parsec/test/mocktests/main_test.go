package mocktests_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestRequests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "requests package internal suite")
}
