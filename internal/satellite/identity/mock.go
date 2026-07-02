package identity

// MockDeviceIdentity implements DeviceIdentity for testing.
type MockDeviceIdentity struct {
	FingerprintValue string
	MACAddressValue  string
	CPUIDValue       string
	BootIDValue      string
	DiskSerialValue  string
	MachineIDValue   string

	FingerprintErr error
	MACAddressErr  error
	CPUIDErr       error
	BootIDErr      error
	DiskSerialErr  error
	MachineIDErr   error
}

// NewMockDeviceIdentity creates a MockDeviceIdentity with default values.
func NewMockDeviceIdentity() *MockDeviceIdentity {
	return &MockDeviceIdentity{
		FingerprintValue: "mock-fingerprint-abc123",
		MACAddressValue:  "00:11:22:33:44:55",
		CPUIDValue:       "mock-cpu-id",
		BootIDValue:      "mock-boot-id-xyz789",
		DiskSerialValue:  "MOCK-DISK-001",
		MachineIDValue:   "mock-machine-id-12345",
	}
}

func (m *MockDeviceIdentity) GetFingerprint() (string, error) {
	if m.FingerprintErr != nil {
		return "", m.FingerprintErr
	}
	return m.FingerprintValue, nil
}

func (m *MockDeviceIdentity) GetMACAddress() (string, error) {
	if m.MACAddressErr != nil {
		return "", m.MACAddressErr
	}
	return m.MACAddressValue, nil
}

func (m *MockDeviceIdentity) GetCPUID() (string, error) {
	if m.CPUIDErr != nil {
		return "", m.CPUIDErr
	}
	return m.CPUIDValue, nil
}

func (m *MockDeviceIdentity) GetBootID() (string, error) {
	if m.BootIDErr != nil {
		return "", m.BootIDErr
	}
	return m.BootIDValue, nil
}

func (m *MockDeviceIdentity) GetDiskSerial() (string, error) {
	if m.DiskSerialErr != nil {
		return "", m.DiskSerialErr
	}
	return m.DiskSerialValue, nil
}

func (m *MockDeviceIdentity) GetMachineID() (string, error) {
	if m.MachineIDErr != nil {
		return "", m.MachineIDErr
	}
	return m.MachineIDValue, nil
}
