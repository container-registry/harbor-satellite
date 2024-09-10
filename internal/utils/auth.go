package utils

import (
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/authn"
)

func Auth() (authn.Authenticator, error) {
	// Get credentials from environment variables
	username := os.Getenv("HARBOR_USERNAME")
	password := os.Getenv("HARBOR_PASSWORD")
	if username == "" || password == "" {
		return nil, fmt.Errorf("HARBOR_USERNAME or HARBOR_PASSWORD environment variable is not set")
	}

	auth := authn.FromConfig(authn.AuthConfig{
		Username: username,
		Password: password,
	})

	return auth, nil
}
