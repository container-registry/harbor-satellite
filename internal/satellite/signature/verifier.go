// Package signature verifies cosign signatures against the satellite's
// signature_policy before an image enters the local store.
package signature

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	sigsig "github.com/sigstore/sigstore/pkg/signature"
)

var _ store.SignatureVerifier = (*Verifier)(nil)

// trustedRootLoader builds the Sigstore trust material for keyless checks.
type trustedRootLoader func(config.KeylessSignatureConfig) (root.TrustedMaterial, error)

// Verifier applies the current signature policy. It rebuilds its checkers
// when the policy changes, so a Ground Control config update applies to the
// next copy without a restart.
type Verifier struct {
	policy   func() config.SignaturePolicyConfig
	loadRoot trustedRootLoader
	// keylessOptions are the sigstore-go checks a keyless signature must pass.
	keylessOptions []verify.VerifierOption
	mu             sync.Mutex
	builtFor       config.SignaturePolicyConfig
	checkers       []checker
	buildError     error
}

// checker verifies a signed entity with one trust anchor: a public key or
// the keyless identity.
type checker struct {
	name     string
	keyless  bool
	verifier *verify.Verifier
	identity verify.PolicyOption
}

// NewVerifier reads the policy through policy on every Verify. tufCacheDir
// holds the Sigstore TUF cache for keyless checks without a trusted_root file.
func NewVerifier(policy func() config.SignaturePolicyConfig, tufCacheDir string) *Verifier {
	return &Verifier{
		policy: policy,
		loadRoot: func(keyless config.KeylessSignatureConfig) (root.TrustedMaterial, error) {
			if keyless.TrustedRoot != "" {
				return root.NewTrustedRootFromPath(keyless.TrustedRoot)
			}
			return root.NewLiveTrustedRoot(tuf.DefaultOptions().WithCachePath(tufCacheDir))
		},
		// The defaults of cosign verify: an SCT, a transparency log entry,
		// and a log or timestamp authority time within the certificate's life.
		keylessOptions: []verify.VerifierOption{
			verify.WithSignedCertificateTimestamps(1),
			verify.WithTransparencyLog(1),
			verify.WithObserverTimestamps(1),
		},
	}
}

// Verify checks the signature candidates of one root. A policy with action
// warn logs a failure and lets the root through.
func (v *Verifier) Verify(ctx context.Context, reference string, signatures store.Signatures) error {
	policy := v.policy()
	if !policy.Enabled {
		return nil
	}
	log := logger.FromContext(ctx).With().Str("reference", reference).
		Str("digest", signatures.Subject.Digest.String()).Str("action", policy.Action).Logger()

	checkers, err := v.checkersFor(policy)
	var signer string
	if err == nil {
		signer, err = verifyAny(ctx, checkers, signatures)
	}
	if err == nil {
		log.Info().Str("signer", signer).Msg("signature verified")
		return nil
	}
	if policy.Action == config.SignatureActionWarn {
		log.Warn().Err(err).Msg("signature verification failed, policy warns only")
		return nil
	}
	log.Error().Err(err).Msg("signature verification failed, artifact blocked")
	return fmt.Errorf("%w: %s: %w", store.ErrSignatureRejected, reference, err)
}

func (v *Verifier) checkersFor(policy config.SignaturePolicyConfig) ([]checker, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if (v.checkers != nil || v.buildError != nil) && reflect.DeepEqual(v.builtFor, policy) {
		return v.checkers, v.buildError
	}
	v.builtFor = policy
	v.checkers, v.buildError = buildCheckers(policy, v.loadRoot, v.keylessOptions)
	// A failed public-good TUF fetch may be a network outage. Try again on
	// the next copy instead of keeping the error.
	if v.buildError != nil && policy.Keyless.Enabled && policy.Keyless.TrustedRoot == "" {
		v.builtFor = config.SignaturePolicyConfig{}
	}
	return v.checkers, v.buildError
}

func buildCheckers(policy config.SignaturePolicyConfig, loadRoot trustedRootLoader, keylessOptions []verify.VerifierOption) ([]checker, error) {
	var checkers []checker
	for i, key := range policy.PublicKeys {
		keyChecker, err := newKeyChecker(key)
		if err != nil {
			return nil, fmt.Errorf("public_keys[%d]: %w", i, err)
		}
		keyChecker.name = fmt.Sprintf("public_keys[%d]", i)
		checkers = append(checkers, keyChecker)
	}
	if policy.Keyless.Enabled {
		keylessChecker, err := newKeylessChecker(policy.Keyless, loadRoot, keylessOptions)
		if err != nil {
			return nil, fmt.Errorf("keyless: %w", err)
		}
		checkers = append(checkers, keylessChecker)
	}
	if len(checkers) == 0 {
		return nil, errors.New("signature policy has no public key and no keyless identity")
	}
	return checkers, nil
}

// newKeyChecker loads an inline PEM key or a PEM file. Key signatures are
// checked without a transparency log, like cosign verify --insecure-ignore-tlog,
// so offline and private signers work.
func newKeyChecker(key string) (checker, error) {
	pemBytes := []byte(key)
	if !strings.Contains(key, "-----BEGIN") {
		var err error
		pemBytes, err = os.ReadFile(key)
		if err != nil {
			return checker{}, fmt.Errorf("read public key: %w", err)
		}
	}
	publicKey, err := cryptoutils.UnmarshalPEMToPublicKey(pemBytes)
	if err != nil {
		return checker{}, fmt.Errorf("parse public key: %w", err)
	}
	keyVerifier, err := sigsig.LoadDefaultVerifier(publicKey)
	if err != nil {
		return checker{}, fmt.Errorf("load public key: %w", err)
	}
	expiring := root.NewExpiringKey(keyVerifier, time.Time{}, time.Time{})
	material := root.NewTrustedPublicKeyMaterial(func(string) (root.TimeConstrainedVerifier, error) {
		return expiring, nil
	})
	sigVerifier, err := verify.NewVerifier(material, verify.WithNoObserverTimestamps())
	if err != nil {
		return checker{}, err
	}
	return checker{verifier: sigVerifier, identity: verify.WithKey()}, nil
}

// newKeylessChecker requires a Fulcio certificate for the configured identity
// that passes options.
func newKeylessChecker(keyless config.KeylessSignatureConfig, loadRoot trustedRootLoader, options []verify.VerifierOption) (checker, error) {
	identity, err := verify.NewShortCertificateIdentity(keyless.Issuer, keyless.IssuerRegexp, keyless.Subject, keyless.SubjectRegexp)
	if err != nil {
		return checker{}, err
	}
	material, err := loadRoot(keyless)
	if err != nil {
		return checker{}, fmt.Errorf("load Sigstore trusted root: %w", err)
	}
	sigVerifier, err := verify.NewVerifier(material, options...)
	if err != nil {
		return checker{}, err
	}
	return checker{name: "keyless", keyless: true, verifier: sigVerifier, identity: verify.WithCertificateIdentity(identity)}, nil
}

// verifyAny accepts the first signature any checker verifies and returns the
// checker name. Otherwise it reports why each candidate failed.
func verifyAny(ctx context.Context, checkers []checker, signatures store.Signatures) (string, error) {
	candidates, failures := loadCandidates(ctx, signatures)
	if len(candidates) == 0 && len(failures) == 0 {
		return "", errors.New("no cosign signature found")
	}
	for _, candidate := range candidates {
		for _, check := range checkers {
			err := candidate.verify(check, signatures.Subject)
			if err == nil {
				return check.name, nil
			}
			failures = append(failures, fmt.Errorf("%s with %s: %w", candidate.name(), check.name, err))
		}
	}
	return "", errors.Join(failures...)
}
