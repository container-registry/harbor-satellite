package runtime

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/viper"
)

const (
	tempRegistriesConfigPath = "/etc/containers/registries.toml"
	registriesConfigPath     = "/etc/containers/registries.conf"
)

func setCrioConfig(upstreamRegistries []string, localMirror string) error {

	if _, err := os.Stat(registriesConfigPath); os.IsNotExist(err) {
		f, err := os.Create(registriesConfigPath)
		if err != nil {
			return fmt.Errorf("error while creating registries.conf : %w", err)
		}
		_ = f.Close()
	}

	// viper fails to recognise .conf file extension, so copy into a temporary .toml file
	if err := copyFile(registriesConfigPath, tempRegistriesConfigPath); err != nil {
		return fmt.Errorf("failed to copy registries.conf file to temporary .toml file: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(tempRegistriesConfigPath)
	v.SetConfigType("toml")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("failed to read registries.conf: %w", err)
		}
	}

	var cfg RegistriesConf
	if err := v.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("failed to unmarshal registries.conf: %w", err)
	}

	insecure := !strings.HasPrefix(localMirror, "https://")

	for _, upstream := range upstreamRegistries {
		// configure only those fields which are not already configured
		registryFound := false
		for i := range cfg.Registries {
			r := &cfg.Registries[i]
			if r.Location == upstream {
				registryFound = true

				mirrorFound := false
				for _, m := range r.Mirrors {
					if m.Location == localMirror {
						mirrorFound = true
						break
					}
				}

				if !mirrorFound {
					r.Mirrors = append(r.Mirrors, Mirror{
						Location: localMirror,
						Insecure: insecure,
					})
				}
				break
			}
		}

		if !registryFound {
			cfg.Registries = append(cfg.Registries, Registry{
				Location: upstream,
				Mirrors: []Mirror{
					{
						Location: localMirror,
						Insecure: insecure,
					},
				},
			})
		}
	}

	v.Set("registry", cfg.Registries)

	if err := v.WriteConfigAs(tempRegistriesConfigPath); err != nil {
		return fmt.Errorf("failed to write registries.conf: %w", err)
	}

	// copy contents of temp file back into actaul path
	if err := copyFile(tempRegistriesConfigPath, registriesConfigPath); err != nil {
		return fmt.Errorf("failed to copy temporary .toml file to registries.conf: %w", err)
	}

	// cleanup : delete temporaary file
	if err := os.Remove(tempRegistriesConfigPath); err != nil {
		return fmt.Errorf("failed to delete temporary .toml file : %w", err)
	}

	return nil
}

// copyFile copies a file from src to dst, replacing dst if it exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = in.Close()
	}()

	out, err := os.Create(dst)
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
