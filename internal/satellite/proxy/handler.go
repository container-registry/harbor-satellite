package proxy

// Handler processes one validated proxy contract.
type Handler interface {
	Handle(*Contract) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(*Contract) error

// Handle calls the adapted handler function.
func (handler HandlerFunc) Handle(contract *Contract) error {
	return handler(contract)
}
