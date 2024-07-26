package reg

import (
	"testing"
)

func BenchmarkFetchRepos(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FetchRepos("admin", "Harbor12345", "https://demo.goharbor.io")
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
