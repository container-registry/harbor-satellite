package proxy

// Predicate decides whether a contract satisfies a reusable domain condition.
type Predicate func(*Contract) bool

type conditionalLayer struct {
	predicate Predicate
	layer     Layer
}

// When applies a layer only when its predicate matches the contract. Invalid
// configuration is reported by New instead of panicking.
func When(predicate Predicate, layer Layer) Layer {
	return conditionalLayer{predicate: predicate, layer: layer}
}

func (conditional conditionalLayer) Wrap(next Handler) Handler {
	if conditional.predicate == nil || conditional.layer == nil {
		return nil
	}
	applied := conditional.layer.Wrap(next)
	if applied == nil {
		return nil
	}
	return HandlerFunc(func(contract *Contract) error {
		if conditional.predicate(contract) {
			return applied.Handle(contract)
		}
		return next.Handle(contract)
	})
}

// Not negates a predicate. It preserves nil so New can reject invalid setup.
func Not(predicate Predicate) Predicate {
	if predicate == nil {
		return nil
	}
	return func(contract *Contract) bool {
		return !predicate(contract)
	}
}

// IsPull matches manifest and blob pulls.
func IsPull(contract *Contract) bool {
	return contract != nil && contract.Access == ReadAccess &&
		(contract.Operation == Manifest || contract.Operation == Blob)
}

// IsBlob matches contracts addressing blob content.
func IsBlob(contract *Contract) bool {
	return contract != nil &&
		(contract.Operation == Blob || contract.Operation == BlobMount)
}

// IsManifest matches contracts addressing a manifest or index.
func IsManifest(contract *Contract) bool {
	return contract != nil && contract.Operation == Manifest
}
