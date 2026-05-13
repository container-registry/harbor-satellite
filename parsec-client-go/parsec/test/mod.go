// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0
package test

import (
	"encoding/base64"
	"io"

	. "github.com/onsi/ginkgo" //nolint // Using for matching and this is idomatic gomega import
	. "github.com/onsi/gomega" //nolint // Using for matching and this is idomatic gomega import
)

// testCase contains test data and used for parsing test cases from json file.
type testCase struct {
	Name     string `json:"name"`
	Request  string `json:"expected_request_binary"`
	Response string `json:"response_binary"`
}

// Implements the Connection interface to allow us to check and inject data during tests
type mockConnection struct {
	responseLookup map[string]string // key = base64 encoded request, value = base64 encoded response
	nextResponse   *string           // copied in if we find a matching request on write - will be read in read then cleared
}

func newMockConnection() *mockConnection {
	return &mockConnection{
		responseLookup: make(map[string]string),
		nextResponse:   nil,
	}
}

func newMockConnectionFromTestCase(testCases []testCase) *mockConnection {
	mc := newMockConnection()
	for _, tc := range testCases {
		mc.responseLookup[tc.Request] = tc.Response
	}
	return mc
}

func (m *mockConnection) Open() error {
	m.nextResponse = nil
	return nil
}

func (m *mockConnection) Read(p []byte) (n int, err error) {
	defer func() { m.nextResponse = nil }()
	if m.nextResponse == nil {
		return 0, io.EOF
	}

	resp, err := base64.StdEncoding.DecodeString(*m.nextResponse)
	if err != nil {
		panic(err)
	}

	return copy(p, resp), nil
}

func (m *mockConnection) Write(p []byte) (n int, err error) {
	encodedOutput := base64.StdEncoding.EncodeToString(p)
	if resp, ok := m.responseLookup[encodedOutput]; ok {
		m.nextResponse = &resp
	}
	Expect(m.nextResponse).NotTo(BeNil())
	return len(p), nil
}

func (m *mockConnection) Close() error {
	return nil
}

// Implements the Connection interface to allow us to check no reads or writes have taken place
type noopConnection struct {
}

func newNoopConnection() *noopConnection {
	return &noopConnection{}
}

func (m *noopConnection) Open() error {
	Fail("Should not have been called")
	return nil
}

func (m *noopConnection) Read(p []byte) (n int, err error) {
	Fail("Should not have been called")
	return 0, nil
}

func (m *noopConnection) Write(p []byte) (n int, err error) {
	Fail("Should not have been called")
	return 0, nil
}

func (m *noopConnection) Close() error {
	Fail("Should not have been called")
	return nil
}
