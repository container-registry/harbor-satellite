package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type HTTPResponse interface {
	GetBody() []byte
	Status() string
	StatusCode() int
}

func ValidateRequired(values ...string) error {
	if len(values)%2 != 0 {
		return fmt.Errorf("required flag validation expects name/value pairs")
	}
	for index := 0; index+1 < len(values); index += 2 {
		if strings.TrimSpace(values[index+1]) == "" {
			return fmt.Errorf("--%s must not be empty", values[index])
		}
	}
	return nil
}

func RequiredAuth(runtime *Runtime) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, _ []string) error {
		return runtime.ValidateAuth()
	}
}

func RequiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s environment variable is required", name)
	}
	return value, nil
}

func MarkRequired(command *cobra.Command, names ...string) {
	for _, name := range names {
		if err := command.MarkFlagRequired(name); err != nil {
			panic(err)
		}
	}
}

func DecodeManifestFile[T any](command *cobra.Command, path string) (T, error) {
	var value T
	var reader io.Reader
	if path == "-" {
		reader = command.InOrStdin()
	} else {
		file, err := os.Open(path)
		if err != nil {
			return value, fmt.Errorf("open request file %q: %w", path, err)
		}
		defer func() { _ = file.Close() }()
		reader = file
	}

	contents, err := io.ReadAll(reader)
	if err != nil {
		return value, fmt.Errorf("read request file %q: %w", path, err)
	}
	if err := yaml.UnmarshalStrict(contents, &value); err != nil {
		return value, fmt.Errorf("decode request file %q: %w", path, err)
	}
	return value, nil
}

func PrintResponse(command *cobra.Command, response HTTPResponse) error {
	if err := ResponseError(response); err != nil {
		return err
	}

	body := bytes.TrimSpace(response.GetBody())
	if len(body) == 0 {
		_, err := fmt.Fprintln(command.OutOrStdout(), response.Status())
		return err
	}
	var formatted bytes.Buffer
	if json.Indent(&formatted, body, "", "  ") == nil {
		body = formatted.Bytes()
	}
	if _, err := command.OutOrStdout().Write(body); err != nil {
		return err
	}
	_, err := fmt.Fprintln(command.OutOrStdout())
	return err
}

func ResponseError(response HTTPResponse) error {
	if response.StatusCode() < http.StatusOK || response.StatusCode() >= http.StatusMultipleChoices {
		body := strings.TrimSpace(string(response.GetBody()))
		if body == "" {
			return fmt.Errorf("ground control returned %s", response.Status())
		}
		return fmt.Errorf("ground control returned %s: %s", response.Status(), body)
	}
	return nil
}
