// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

module github.com/parallaxsecond/parsec-client-go

// Go 1.21 floor: required by google.golang.org/protobuf >= v1.33 which fixes
// CVE-2024-24786 (protojson.Unmarshal infinite loop).
go 1.23

require (
	github.com/onsi/ginkgo v1.15.0
	github.com/onsi/gomega v1.10.5
	github.com/pkg/errors v0.9.1
	// CVE-2024-24786 (protojson.Unmarshal infinite loop) fixed in v1.33+
	google.golang.org/protobuf v1.36.10
)

require (
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/nxadm/tail v1.4.4 // indirect
	golang.org/x/net v0.0.0-20201202161906-c7110b5ffcbb // indirect
	// CVE-2022-29526 (faccessat group check) fixed in newer x/sys
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.3.3 // indirect
	gopkg.in/check.v1 v1.0.0-20200902074654-038fdea0a05b // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.3.0 // indirect
)
