package proxy

// Layer decorates contract processing and explicitly controls continuation.
type Layer interface {
	Wrap(Handler) Handler
}

// LayerFunc adapts a handler decorator function to Layer.
type LayerFunc func(Handler) Handler

// Wrap calls the adapted layer function.
func (layer LayerFunc) Wrap(next Handler) Handler {
	if layer == nil {
		return nil
	}
	return layer(next)
}
