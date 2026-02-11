package identity

import "errors"

var (
	ErrComponentUnavailable = errors.New("identity component unavailable")
	ErrFingerprintFailed    = errors.New("failed to generate fingerprint")
)

// DeviceIdentity provides device identification for fingerprinting.
// The fingerprint is used to derive encryption keys and validate device identity.
type DeviceIdentity interface {
	// GetFingerprint returns a unique device fingerprint derived from
	// multiple hardware identifiers. The fingerprint should be consistent
	// across reboots but change if hardware is replaced.
	GetFingerprint() (string, error)

	// GetMACAddress returns the primary network interface MAC address.
	GetMACAddress() (string, error)

	// GetCPUID returns a CPU identifier if available.
	GetCPUID() (string, error)

	// GetBootID returns the current boot ID.
	// This changes on every reboot.
	GetBootID() (string, error)

	// GetDiskSerial returns the primary disk serial number.
	GetDiskSerial() (string, error)

	// GetMachineID returns the persistent machine ID (/etc/machine-id).
	GetMachineID() (string, error)
}
