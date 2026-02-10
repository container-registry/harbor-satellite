package runtime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/viper"
)

const (
	tempRegistriesConfigPath = "/etc/containers/registries.toml"
	registriesConfigPath     = "/etc/containers/registries.conf"
)

func setCrioConfig(upstreamRegistries []string, localMirror string) (string, error) {
	if _, err := os.Stat(registriesConfigPath); os.IsNotExist(err) {
		f, err := os.Create(registriesConfigPath)
		if err != nil {
			return "", fmt.Errorf("error creating registries.conf: %w", err)
		}
		_ = f.Close()
	}

	bkPath, err := backupFile(registriesConfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to backup registries.conf: %w", err)
	}

	// viper fails to recognise .conf file extension, so copy into a temporary .toml file
	if err := copyFile(registriesConfigPath, tempRegistriesConfigPath); err != nil {
		return bkPath, fmt.Errorf("failed to copy registries.conf file to temporary .toml file: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(tempRegistriesConfigPath)
	v.SetConfigType("toml")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return bkPath, fmt.Errorf("failed to read registries.conf: %w", err)
		}
	}

	var cfg RegistriesConf
	if err := v.Unmarshal(&cfg); err != nil {
		return bkPath, fmt.Errorf("failed to unmarshal registries.conf: %w", err)
	}

	insecure := !strings.HasPrefix(localMirror, "https://")

	for _, upstream := range upstreamRegistries {
		idx := slices.IndexFunc(cfg.Registries, func(r Registry) bool {
			return r.Location == upstream
		})

		if idx >= 0 {
			r := &cfg.Registries[idx]
			hasMirror := slices.ContainsFunc(r.Mirrors, func(m Mirror) bool {
				return m.Location == localMirror
			})
			if !hasMirror {
				r.Mirrors = append(r.Mirrors, Mirror{Location: localMirror, Insecure: insecure})
			}
		} else {
			cfg.Registries = append(cfg.Registries, Registry{
				Location: upstream,
				Mirrors:  []Mirror{{Location: localMirror, Insecure: insecure}},
			})
		}
	}

	v.Set("registry", cfg.Registries)

	if err := v.WriteConfigAs(tempRegistriesConfigPath); err != nil {
		return bkPath, fmt.Errorf("failed to write registries.conf: %w", err)
	}

	// validate TOML before committing
	data, err := os.ReadFile(tempRegistriesConfigPath)
	if err != nil {
		return bkPath, fmt.Errorf("failed to read temp registries file: %w", err)
	}
	if err := validateTOML(data); err != nil {
		if bkPath != "" {
			_ = restoreBackup(bkPath, registriesConfigPath)
		}
		return bkPath, fmt.Errorf("registries.conf validation failed, rolled back: %w", err)
	}

	// copy contents of temp file back into actual path
	if err := copyFile(tempRegistriesConfigPath, registriesConfigPath); err != nil {
		return bkPath, fmt.Errorf("failed to copy temporary .toml file to registries.conf: %w", err)
	}

	// cleanup: delete temporary file (non-fatal)
	_ = os.Remove(tempRegistriesConfigPath)

	return bkPath, nil
}

// copyFile copies a file from src to dst, replacing dst if it exists.
func copyFile(src, dst string) error {
	in, err := os.Open(filepath.Clean(src))
	if err != nil {
		return err
	}
	defer func() {
		_ = in.Close()
	}()

	out, err := os.Create(filepath.Clean(dst))
	if err != nil {
		return err
	}
	defer func(){
		_ = out.Close()
	}()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
