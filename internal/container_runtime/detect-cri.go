package runtime

import (
	"fmt"
	"strings"
)

// CRIConfig pairs a CRI type with the registries it should mirror.
type CRIConfig struct {
	CRI        CRIType
	Registries []string
}

// CRIConfigResult holds the outcome of applying a single CRI config.
type CRIConfigResult struct {
	CRI        CRIType
	BackupPath string
	Success    bool
	Error      string
}

// ResolveCRIConfigs determines which CRI configs to apply.
// Priority: explicitMirrors (--mirrors flag) > explicit runtimes > auto-detect.
func ResolveCRIConfigs(explicitMirrors []string, autoDetect bool, registries []string, runtimes []string) ([]CRIConfig, error) {
	// If explicit --mirrors provided, parse and return as-is (legacy behavior)
	if len(explicitMirrors) > 0 {
		return parseMirrorFlags(explicitMirrors)
	}

	if !autoDetect {
		return nil, nil
	}

	var criTypes []CRIType
	if len(runtimes) > 0 {
		for _, rt := range runtimes {
			criTypes = append(criTypes, CRIType(rt))
		}
	} else {
		detected := DetectInstalledCRIs()
		for _, d := range detected {
			criTypes = append(criTypes, d.Type)
		}
	}

	var configs []CRIConfig
	for _, cri := range criTypes {
		regs := registries
		// Docker only supports docker.io mirroring via true/false
		if cri == CRIDocker {
			regs = []string{"true"}
		}
		configs = append(configs, CRIConfig{CRI: cri, Registries: regs})
	}

	return configs, nil
}

// parseMirrorFlags parses --mirrors flag values into CRIConfigs.
func parseMirrorFlags(mirrors []string) ([]CRIConfig, error) {
	var configs []CRIConfig
	for _, entry := range mirrors {
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid mirror format: %s", entry)
		}
		configs = append(configs, CRIConfig{
			CRI:        CRIType(parts[0]),
			Registries: strings.Split(parts[1], ","),
		})
	}
	return configs, nil
}

// ApplyCRIConfigs applies the given CRI configs and returns results.
// Errors are collected per-CRI rather than failing on the first error.
func ApplyCRIConfigs(configs []CRIConfig, localRegistry string) []CRIConfigResult {
	var results []CRIConfigResult

	for _, cfg := range configs {
		result := CRIConfigResult{CRI: cfg.CRI}

		var backupPath string
		var err error

		switch cfg.CRI {
		case CRIDocker:
			backupPath, err = setDockerdConfig(cfg.Registries, localRegistry)
		case CRICrio, CRIPodman:
			backupPath, err = setCrioConfig(cfg.Registries, localRegistry)
		case CRIContainerd:
			backupPath, err = setContainerdConfig(cfg.Registries, localRegistry)
		default:
			err = fmt.Errorf("unsupported CRI: %s", cfg.CRI)
		}

		result.BackupPath = backupPath
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Success = true
		}

		results = append(results, result)
	}

	return results
}
