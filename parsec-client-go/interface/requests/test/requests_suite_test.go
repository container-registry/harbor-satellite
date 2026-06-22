// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package requests_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestRequests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "requests package internal suite")
}
