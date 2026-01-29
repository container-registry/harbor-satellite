//go:build nospiffe

package identity

// NoOpIdentity provides no-op device identity for minimal builds.
type NoOpIdentity struct{}

func NewNoOpIdentity() *NoOpIdentity {
	return &NoOpIdentity{}
}

func NewLinuxDeviceIdentity() *NoOpIdentity {
	return &NoOpIdentity{}
}

func (d *NoOpIdentity) GetFingerprint() (string, error) {
	return "", ErrComponentUnavailable
}

func (d *NoOpIdentity) GetMACAddress() (string, error) {
	return "", ErrComponentUnavailable
}

func (d *NoOpIdentity) GetCPUID() (string, error) {
	return "", ErrComponentUnavailable
}

func (d *NoOpIdentity) GetBootID() (string, error) {
	return "", ErrComponentUnavailable
}

func (d *NoOpIdentity) GetDiskSerial() (string, error) {
	return "", ErrComponentUnavailable
}

func (d *NoOpIdentity) GetMachineID() (string, error) {
	return "", ErrComponentUnavailable
}
