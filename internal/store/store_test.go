package store

import (
	"context"
	"testing"
)

// Benchmark for the uncommented (sync.Map-based) version
func BenchmarkSyncMapStore(b *testing.B) {
	store := NewInMemoryStore().(*inMemoryStore)
	ctx := context.Background()
	img := Image{Reference: "example:latest"}

	// Benchmark List
	b.Run("List", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			store.List(ctx)
		}
	})

	// Benchmark Add
	b.Run("Add", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			store.Add(ctx, img)
		}
	})

	// Benchmark Remove
	b.Run("Remove", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			store.Remove(ctx, img.Reference)
		}
	})
}

// Benchmark for the commented (map-based) version
func BenchmarkMapStore(b *testing.B) {
	store := NewInMemoryStore().(*inMemoryStore)
	ctx := context.Background()
	img := Image{Reference: "example:latest"}

	// Benchmark List
	b.Run("List", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			store.List(ctx)
		}
	})

	// Benchmark Add
	b.Run("Add", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			store.Add(ctx, img)
		}
	})

	// Benchmark Remove
	b.Run("Remove", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			store.Remove(ctx, img.Reference)
		}
	})
}
