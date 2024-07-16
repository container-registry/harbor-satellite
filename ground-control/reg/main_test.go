package main

import (
	"context"
	"testing"
)

func BenchmarkListRepos(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ListRepos(context.Background())
	}
}

// Benchmark Result:
// goos: linux
// goarch: amd64
// pkg: container-registry.com/harbor-satellite/ground-control/reg
// cpu: AMD Ryzen 5 5600X 6-Core Processor
// BenchmarkListRepos-12               1071           1028679 ns/op
// PASS
// ok      container-registry.com/harbor-satellite/ground-control/reg      2.237s

