package main

import (
	"context"
	"testing"
)

func BenchmarkHarborList(b *testing.B) {
	for i := 0; i < b.N; i++ {
		HarborList(context.Background())
	}
}

// BenchMark Result:
// goos: linux
// goarch: amd64
// pkg: container-registry.com/harbor-satellite/ground-control/reg
// cpu: AMD Ryzen 5 5600X 6-Core Processor
// BenchmarkListRepos-12               657           1826842 ns/op
// PASS
// ok      container-registry.com/harbor-satellite/ground-control/reg/adapter      1.398s

