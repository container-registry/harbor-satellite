package identity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMockDeviceIdentity_GetFingerprint(t *testing.T) {
	t.Run("returns fingerprint successfully", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		fp, err := m.GetFingerprint()
		require.NoError(t, err)
		require.Equal(t, "mock-fingerprint-abc123", fp)
	})

	t.Run("returns error when configured", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		m.FingerprintErr = ErrFingerprintFailed
		_, err := m.GetFingerprint()
		require.ErrorIs(t, err, ErrFingerprintFailed)
	})

	t.Run("fingerprint is consistent", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		fp1, err := m.GetFingerprint()
		require.NoError(t, err)
		fp2, err := m.GetFingerprint()
		require.NoError(t, err)
		require.Equal(t, fp1, fp2)
	})

	t.Run("fingerprint changes on hardware change", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		fp1, err := m.GetFingerprint()
		require.NoError(t, err)

		m.FingerprintValue = "new-hardware-fingerprint"
		fp2, err := m.GetFingerprint()
		require.NoError(t, err)

		require.NotEqual(t, fp1, fp2)
	})
}

func TestMockDeviceIdentity_GetMACAddress(t *testing.T) {
	t.Run("returns MAC address successfully", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		mac, err := m.GetMACAddress()
		require.NoError(t, err)
		require.Equal(t, "00:11:22:33:44:55", mac)
	})

	t.Run("returns error when unavailable", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		m.MACAddressErr = ErrComponentUnavailable
		_, err := m.GetMACAddress()
		require.ErrorIs(t, err, ErrComponentUnavailable)
	})
}

func TestMockDeviceIdentity_GetCPUID(t *testing.T) {
	t.Run("returns CPU ID successfully", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		cpuid, err := m.GetCPUID()
		require.NoError(t, err)
		require.Equal(t, "mock-cpu-id", cpuid)
	})

	t.Run("returns error when unavailable", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		m.CPUIDErr = ErrComponentUnavailable
		_, err := m.GetCPUID()
		require.ErrorIs(t, err, ErrComponentUnavailable)
	})
}

func TestMockDeviceIdentity_GetBootID(t *testing.T) {
	t.Run("returns boot ID successfully", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		bootID, err := m.GetBootID()
		require.NoError(t, err)
		require.Equal(t, "mock-boot-id-xyz789", bootID)
	})

	t.Run("returns error when unavailable", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		m.BootIDErr = ErrComponentUnavailable
		_, err := m.GetBootID()
		require.ErrorIs(t, err, ErrComponentUnavailable)
	})
}

func TestMockDeviceIdentity_GetDiskSerial(t *testing.T) {
	t.Run("returns disk serial successfully", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		serial, err := m.GetDiskSerial()
		require.NoError(t, err)
		require.Equal(t, "MOCK-DISK-001", serial)
	})

	t.Run("returns error when unavailable", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		m.DiskSerialErr = ErrComponentUnavailable
		_, err := m.GetDiskSerial()
		require.ErrorIs(t, err, ErrComponentUnavailable)
	})
}

func TestMockDeviceIdentity_GetMachineID(t *testing.T) {
	t.Run("returns machine ID successfully", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		machineID, err := m.GetMachineID()
		require.NoError(t, err)
		require.Equal(t, "mock-machine-id-12345", machineID)
	})

	t.Run("returns error when unavailable", func(t *testing.T) {
		m := NewMockDeviceIdentity()
		m.MachineIDErr = ErrComponentUnavailable
		_, err := m.GetMachineID()
		require.ErrorIs(t, err, ErrComponentUnavailable)
	})
}

func TestMockDeviceIdentity_AllComponentsPresent(t *testing.T) {
	m := NewMockDeviceIdentity()

	mac, err := m.GetMACAddress()
	require.NoError(t, err)
	require.NotEmpty(t, mac)

	cpuid, err := m.GetCPUID()
	require.NoError(t, err)
	require.NotEmpty(t, cpuid)

	bootID, err := m.GetBootID()
	require.NoError(t, err)
	require.NotEmpty(t, bootID)

	diskSerial, err := m.GetDiskSerial()
	require.NoError(t, err)
	require.NotEmpty(t, diskSerial)

	machineID, err := m.GetMachineID()
	require.NoError(t, err)
	require.NotEmpty(t, machineID)

	fp, err := m.GetFingerprint()
	require.NoError(t, err)
	require.NotEmpty(t, fp)
}
