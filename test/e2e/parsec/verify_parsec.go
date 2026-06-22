//go:build parsec

package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/parallaxsecond/parsec-client-go/interface/requests"
	parsecclient "github.com/parallaxsecond/parsec-client-go/parsec"
	"github.com/parallaxsecond/parsec-client-go/parsec/algorithm"
)

const identityKeyName = "satellite-identity-key"

func main() {
	socketPath := flag.String("socket", "/run/parsec/parsec.sock", "PARSEC daemon socket path")
	wait := flag.Duration("wait", 30*time.Second, "maximum time to wait for the satellite identity key")
	flag.Parse()

	if err := verify(*socketPath, *wait); err != nil {
		fmt.Fprintf(os.Stderr, "PARSEC E2E verification failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("PARSEC E2E verification passed: identity key signs, verifies, and remains non-exportable")
}

func verify(socketPath string, wait time.Duration) error {
	if socketPath == "" {
		return fmt.Errorf("socket path must not be empty")
	}
	if err := os.Setenv("PARSEC_SERVICE_ENDPOINT", "unix:"+socketPath); err != nil {
		return fmt.Errorf("set PARSEC_SERVICE_ENDPOINT: %w", err)
	}

	client, err := parsecclient.CreateConfiguredClient(parsecclient.NewClientConfig())
	if err != nil {
		return fmt.Errorf("connect to parsec daemon at %s: %w", socketPath, err)
	}
	defer client.Close() //nolint:errcheck

	if _, _, err := client.Ping(); err != nil {
		return fmt.Errorf("ping parsec daemon: %w", err)
	}

	if err := waitForIdentityKey(client, wait); err != nil {
		return err
	}
	if err := verifySignRoundTrip(client); err != nil {
		return err
	}
	if err := verifyPrivateKeyNonExportable(client); err != nil {
		return err
	}

	return nil
}

func waitForIdentityKey(client *parsecclient.BasicClient, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	var lastErr error

	for time.Now().Before(deadline) {
		found, err := hasIdentityKey(client)
		if err == nil && found {
			return nil
		}
		lastErr = err
		time.Sleep(time.Second)
	}

	if lastErr != nil {
		return fmt.Errorf("wait for %q: %w", identityKeyName, lastErr)
	}
	return fmt.Errorf("wait for %q: key not found after %s", identityKeyName, wait)
}

func hasIdentityKey(client *parsecclient.BasicClient) (bool, error) {
	keys, err := client.ListKeys()
	if err != nil {
		return false, fmt.Errorf("list parsec keys: %w", err)
	}
	for _, key := range keys {
		if key.Name == identityKeyName {
			return true, nil
		}
	}
	return false, nil
}

func verifySignRoundTrip(client *parsecclient.BasicClient) error {
	digest := sha256.Sum256([]byte("harbor-satellite-parsec-e2e"))
	sigAlg := algorithm.NewAsymmetricSignature().
		Ecdsa(algorithm.HashAlgorithmTypeSHA256).
		GetAsymmetricSignature()

	sig, err := client.PsaSignHash(identityKeyName, digest[:], sigAlg)
	if err != nil {
		return fmt.Errorf("sign with parsec identity key: %w", err)
	}
	if len(sig) == 0 {
		return fmt.Errorf("sign with parsec identity key: empty signature")
	}
	if err := client.PsaVerifyHash(identityKeyName, digest[:], sig, sigAlg); err != nil {
		return fmt.Errorf("verify parsec identity signature: %w", err)
	}

	tampered := sha256.Sum256([]byte("harbor-satellite-parsec-e2e-tampered"))
	verifyErr := client.PsaVerifyHash(identityKeyName, tampered[:], sig, sigAlg)
	if err := expectStatus(verifyErr, requests.StatusPsaErrorInvalidSignature, "verify parsec identity signature with tampered digest"); err != nil {
		return err
	}

	return nil
}

func verifyPrivateKeyNonExportable(client *parsecclient.BasicClient) error {
	if _, err := client.PsaExportPublicKey(identityKeyName); err != nil {
		return fmt.Errorf("export parsec identity public key: %w", err)
	}
	_, err := client.PsaExportKey(identityKeyName)
	if err := expectStatus(err, requests.StatusPsaErrorNotPermitted, "export parsec identity private key"); err != nil {
		return err
	}
	return nil
}

func expectStatus(err error, status requests.StatusCode, action string) error {
	expected := status.ToErr()
	if expected == nil {
		return fmt.Errorf("%s: expected non-success status %d", action, status)
	}
	if err == nil {
		return fmt.Errorf("%s: unexpectedly succeeded", action)
	}

	// The client maps response statuses to untyped errors, so compare the canonical mapping.
	if err.Error() != expected.Error() {
		return fmt.Errorf("%s: got %v, want %v", action, err, expected)
	}
	return nil
}
