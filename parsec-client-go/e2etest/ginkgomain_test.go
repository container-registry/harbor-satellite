//go:build end2endtest
// +build end2endtest

// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package e2etest

// Test case to run Ginkgo tests

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestRequests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "requests package internal suite")
}
