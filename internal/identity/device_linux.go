//go:build linux && !nospiffe

package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const (
	machineIDPath = "/etc/machine-id"
	bootIDPath    = "/proc/sys/kernel/random/boot_id"
	cpuInfoPath   = "/proc/cpuinfo"
	blockDevPath  = "/sys/class/block"
)

// LinuxDeviceIdentity implements DeviceIdentity for Linux systems.
type LinuxDeviceIdentity struct {
	machineIDPath string
	bootIDPath    string
	cpuInfoPath   string
	blockDevPath  string
}

// NewLinuxDeviceIdentity creates a new LinuxDeviceIdentity with default paths.
func NewLinuxDeviceIdentity() *LinuxDeviceIdentity {
	return &LinuxDeviceIdentity{
		machineIDPath: machineIDPath,
		bootIDPath:    bootIDPath,
		cpuInfoPath:   cpuInfoPath,
		blockDevPath:  blockDevPath,
	}
}

// GetFingerprint generates a unique device fingerprint from hardware identifiers.
// The fingerprint is a SHA-256 hash of machine ID, MAC address, and disk serial.
func (d *LinuxDeviceIdentity) GetFingerprint() (string, error) {
	components := make([]string, 0, 3)

	machineID, err := d.GetMachineID()
	if err == nil && machineID != "" {
		components = append(components, machineID)
	}

	mac, err := d.GetMACAddress()
	if err == nil && mac != "" {
		components = append(components, mac)
	}

	diskSerial, err := d.GetDiskSerial()
	if err == nil && diskSerial != "" {
		components = append(components, diskSerial)
	}

	if len(components) == 0 {
		return "", ErrFingerprintFailed
	}

	combined := strings.Join(components, "|")
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:]), nil
}

// GetMACAddress returns the primary network interface MAC address.
func (d *LinuxDeviceIdentity) GetMACAddress() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("list interfaces: %w", err)
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		return iface.HardwareAddr.String(), nil
	}

	return "", ErrComponentUnavailable
}

// GetCPUID returns a CPU identifier from /proc/cpuinfo.
func (d *LinuxDeviceIdentity) GetCPUID() (string, error) {
	data, err := os.ReadFile(d.cpuInfoPath)
	if err != nil {
		return "", fmt.Errorf("read cpuinfo: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Serial") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	return "", ErrComponentUnavailable
}

// GetBootID returns the current boot ID.
func (d *LinuxDeviceIdentity) GetBootID() (string, error) {
	data, err := os.ReadFile(d.bootIDPath)
	if err != nil {
		return "", fmt.Errorf("read boot_id: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// GetDiskSerial returns the primary disk serial number.
func (d *LinuxDeviceIdentity) GetDiskSerial() (string, error) {
	entries, err := os.ReadDir(d.blockDevPath)
	if err != nil {
		return "", fmt.Errorf("read block devices: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "sd") && !strings.HasPrefix(name, "nvme") &&
			!strings.HasPrefix(name, "vd") && !strings.HasPrefix(name, "mmcblk") {
			continue
		}
		if strings.Contains(name, "p") && strings.HasPrefix(name, "nvme") {
			continue
		}
		if len(name) > 3 && name[2] >= '0' && name[2] <= '9' && strings.HasPrefix(name, "sd") {
			continue
		}

		serialPath := filepath.Join(d.blockDevPath, name, "device", "serial")
		data, err := os.ReadFile(serialPath)
		if err == nil {
			serial := strings.TrimSpace(string(data))
			if serial != "" {
				return serial, nil
			}
		}
	}

	return "", ErrComponentUnavailable
}

// GetMachineID returns the persistent machine ID.
func (d *LinuxDeviceIdentity) GetMachineID() (string, error) {
	data, err := os.ReadFile(d.machineIDPath)
	if err != nil {
		return "", fmt.Errorf("read machine-id: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
