//go:build linux

package identity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLinuxDeviceIdentity_GetFingerprint(t *testing.T) {
	d := NewLinuxDeviceIdentity()

	t.Run("generates fingerprint successfully", func(t *testing.T) {
		fp, err := d.GetFingerprint()
		require.NoError(t, err)
		require.NotEmpty(t, fp)
		require.Len(t, fp, 64) // SHA-256 hex
	})

	t.Run("fingerprint is consistent", func(t *testing.T) {
		fp1, err := d.GetFingerprint()
		require.NoError(t, err)

		fp2, err := d.GetFingerprint()
		require.NoError(t, err)

		require.Equal(t, fp1, fp2)
	})
}

func TestLinuxDeviceIdentity_GetMACAddress(t *testing.T) {
	d := NewLinuxDeviceIdentity()

	t.Run("returns MAC address", func(t *testing.T) {
		mac, err := d.GetMACAddress()
		if err == ErrComponentUnavailable {
			t.Skip("no network interface available")
		}
		require.NoError(t, err)
		require.NotEmpty(t, mac)
		require.Contains(t, mac, ":")
	})
}

func TestLinuxDeviceIdentity_GetCPUID(t *testing.T) {
	d := NewLinuxDeviceIdentity()

	t.Run("returns CPU info", func(t *testing.T) {
		cpuid, err := d.GetCPUID()
		if err == ErrComponentUnavailable {
			t.Skip("CPU ID not available")
		}
		require.NoError(t, err)
		require.NotEmpty(t, cpuid)
	})
}

func TestLinuxDeviceIdentity_GetBootID(t *testing.T) {
	d := NewLinuxDeviceIdentity()

	t.Run("returns boot ID", func(t *testing.T) {
		bootID, err := d.GetBootID()
		require.NoError(t, err)
		require.NotEmpty(t, bootID)
	})
}

func TestLinuxDeviceIdentity_GetDiskSerial(t *testing.T) {
	d := NewLinuxDeviceIdentity()

	t.Run("returns disk serial or unavailable", func(t *testing.T) {
		serial, err := d.GetDiskSerial()
		if err == ErrComponentUnavailable {
			t.Skip("disk serial not available")
		}
		require.NoError(t, err)
		require.NotEmpty(t, serial)
	})
}

func TestLinuxDeviceIdentity_GetMachineID(t *testing.T) {
	d := NewLinuxDeviceIdentity()

	t.Run("returns machine ID", func(t *testing.T) {
		machineID, err := d.GetMachineID()
		require.NoError(t, err)
		require.NotEmpty(t, machineID)
	})
}

func TestLinuxDeviceIdentity_WithMockedPaths(t *testing.T) {
	tmpDir := t.TempDir()

	machineIDPath := filepath.Join(tmpDir, "machine-id")
	bootIDPath := filepath.Join(tmpDir, "boot_id")

	require.NoError(t, os.WriteFile(machineIDPath, []byte("test-machine-id-12345\n"), 0o644))
	require.NoError(t, os.WriteFile(bootIDPath, []byte("test-boot-id-xyz\n"), 0o644))

	d := &LinuxDeviceIdentity{
		machineIDPath: machineIDPath,
		bootIDPath:    bootIDPath,
		cpuInfoPath:   "/proc/cpuinfo",
		blockDevPath:  "/sys/class/block",
	}

	t.Run("reads mocked machine ID", func(t *testing.T) {
		machineID, err := d.GetMachineID()
		require.NoError(t, err)
		require.Equal(t, "test-machine-id-12345", machineID)
	})

	t.Run("reads mocked boot ID", func(t *testing.T) {
		bootID, err := d.GetBootID()
		require.NoError(t, err)
		require.Equal(t, "test-boot-id-xyz", bootID)
	})
}

func TestLinuxDeviceIdentity_FingerprintFallback(t *testing.T) {
	tmpDir := t.TempDir()

	machineIDPath := filepath.Join(tmpDir, "machine-id")
	require.NoError(t, os.WriteFile(machineIDPath, []byte("fallback-machine-id\n"), 0o644))

	d := &LinuxDeviceIdentity{
		machineIDPath: machineIDPath,
		bootIDPath:    filepath.Join(tmpDir, "nonexistent"),
		cpuInfoPath:   filepath.Join(tmpDir, "nonexistent"),
		blockDevPath:  filepath.Join(tmpDir, "nonexistent"),
	}

	fp, err := d.GetFingerprint()
	require.NoError(t, err)
	require.NotEmpty(t, fp)
	require.Len(t, fp, 64)
}
