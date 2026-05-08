package policy

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"os"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const cosignSigAnnotation = "dev.cosignproject.cosign/signature"

// Action controls what happens when signature verification fails.
type Action string

const (
	// ActionBlock rejects unsigned images and halts replication.
	ActionBlock Action = "block"
	// ActionWarn logs a warning but allows replication to continue.
	ActionWarn Action = "warn"
)

// Config is the signature_policy subsection of AppConfig.
type Config struct {
	Enabled    bool     `json:"enabled,omitempty"`
	PublicKeys []string `json:"public_keys,omitempty"`
	Action     Action   `json:"action,omitempty"`
}

// Verifier checks an image's cosign signature before it is replicated.
type Verifier interface {
	Verify(ctx context.Context, imageRef string, insecure bool, username, password string) error
}

type nopVerifier struct{}

func (nopVerifier) Verify(_ context.Context, _ string, _ bool, _, _ string) error { return nil }

// New returns a Verifier for the given Config.
// Returns a no-op verifier when cfg.Enabled is false.
func New(cfg Config) (Verifier, error) {
	if !cfg.Enabled {
		return nopVerifier{}, nil
	}
	if len(cfg.PublicKeys) == 0 {
		return nil, fmt.Errorf("signature_policy: at least one public_key path is required when enabled")
	}
	if cfg.Action == "" {
		cfg.Action = ActionBlock
	}
	if cfg.Action != ActionBlock && cfg.Action != ActionWarn {
		return nil, fmt.Errorf("signature_policy: invalid action %q, must be %q or %q", cfg.Action, ActionBlock, ActionWarn)
	}

	keys := make([]*ecdsa.PublicKey, 0, len(cfg.PublicKeys))
	for _, path := range cfg.PublicKeys {
		k, err := loadECPublicKey(path)
		if err != nil {
			return nil, fmt.Errorf("signature_policy: load key %s: %w", path, err)
		}
		keys = append(keys, k)
	}
	return &cosignVerifier{cfg: cfg, keys: keys}, nil
}

type cosignVerifier struct {
	cfg  Config
	keys []*ecdsa.PublicKey
}

// Verify fetches the cosign signature artifact for imageRef from the source
// registry and checks it against each configured public key. Returns nil when
// at least one key verifies a signature.
//
// Signature artifacts follow the cosign storage convention:
//
//	<registry>/<repo>/<name>:sha256-<hex>.sig
//
// Each layer in the signature manifest carries the ECDSA sig (DER, base64) in
// the annotation dev.cosignproject.cosign/signature; the layer blob is the
// simple-signing payload that was signed.
func (v *cosignVerifier) Verify(ctx context.Context, imageRef string, insecure bool, username, password string) error {
	var nameOpts []name.Option
	if insecure {
		nameOpts = append(nameOpts, name.Insecure)
	}

	opts := []remote.Option{remote.WithContext(ctx)}
	if username != "" || password != "" {
		opts = append(opts, remote.WithAuth(authn.FromConfig(authn.AuthConfig{
			Username: username,
			Password: password,
		})))
	}

	ref, err := name.ParseReference(imageRef, nameOpts...)
	if err != nil {
		return fmt.Errorf("parse ref %q: %w", imageRef, err)
	}

	head, err := remote.Head(ref, opts...)
	if err != nil {
		return fmt.Errorf("resolve digest for %q: %w", imageRef, err)
	}

	sigTagStr := fmt.Sprintf("sha256-%s.sig", head.Digest.Hex)
	sigRef, err := name.ParseReference(
		fmt.Sprintf("%s:%s", ref.Context().String(), sigTagStr),
		nameOpts...,
	)
	if err != nil {
		return fmt.Errorf("build sig ref: %w", err)
	}

	sigImg, err := remote.Image(sigRef, opts...)
	if err != nil {
		return v.handleMissing(imageRef, fmt.Errorf("fetch signature artifact: %w", err))
	}

	manifest, err := sigImg.Manifest()
	if err != nil {
		return v.handleMissing(imageRef, fmt.Errorf("get signature manifest: %w", err))
	}

	if len(manifest.Layers) == 0 {
		return v.handleMissing(imageRef, fmt.Errorf("no signatures found for %s", imageRef))
	}

	for _, layerDesc := range manifest.Layers {
		if err := v.verifyLayer(sigImg, layerDesc); err == nil {
			return nil
		}
	}

	return v.handleMissing(imageRef, fmt.Errorf("no valid cosign signature found for %s", imageRef))
}

func (v *cosignVerifier) verifyLayer(img v1.Image, desc v1.Descriptor) error {
	sigB64, ok := desc.Annotations[cosignSigAnnotation]
	if !ok {
		return fmt.Errorf("layer missing signature annotation")
	}

	sigDER, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	layer, err := img.LayerByDigest(desc.Digest)
	if err != nil {
		return fmt.Errorf("fetch layer: %w", err)
	}

	// The simple-signing payload blob is pushed uncompressed (raw JSON);
	// Compressed() returns the stored bytes as-is from the registry.
	rc, err := layer.Compressed()
	if err != nil {
		return fmt.Errorf("open layer: %w", err)
	}
	defer rc.Close()

	payload, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read layer: %w", err)
	}

	hash := sha256.Sum256(payload)

	for _, key := range v.keys {
		if ecdsa.VerifyASN1(key, hash[:], sigDER) {
			return nil
		}
	}
	return fmt.Errorf("signature does not match any configured key")
}

func (v *cosignVerifier) handleMissing(imageRef string, cause error) error {
	if v.cfg.Action == ActionWarn {
		return &WarnError{Ref: imageRef, cause: cause}
	}
	return cause
}

// WarnError is returned when Action is "warn" and verification fails.
// Callers can check with IsWarnError to treat it as a non-fatal condition.
type WarnError struct {
	Ref   string
	cause error
}

func (e *WarnError) Error() string { return fmt.Sprintf("unverified image %s (warn-only): %v", e.Ref, e.cause) }
func (e *WarnError) Unwrap() error { return e.cause }

// IsWarnError reports whether err is a non-fatal signature warning.
func IsWarnError(err error) bool {
	_, ok := err.(*WarnError)
	return ok
}

func loadECPublicKey(path string) (*ecdsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %s", path)
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX key: %w", err)
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("expected ECDSA public key, got %T", pub)
	}
	return ecPub, nil
}
