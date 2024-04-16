package store

import "context"

type Image struct {
	Reference string
}

type Storer interface {
	// Lists all images that should be replicated.
	List(ctx context.Context) ([]Image, error)
}
