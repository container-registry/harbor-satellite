package signature

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/pkg/config"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/sign"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/errdef"
)

const (
	testIssuer      = "https://issuer.example"
	testIdentity    = "ci@example.com"
	testBundleMedia = "application/vnd.dev.sigstore.bundle.v0.3+json"
	testInTotoType  = "application/vnd.in-toto+json"
)

var testSubject = ocispec.Descriptor{
	MediaType: ocispec.MediaTypeImageManifest,
	Digest:    digest.FromString("the signed image"),
	Size:      42,
}

func policyWithKeys(action string, keys ...string) func() config.SignaturePolicyConfig {
	return func() config.SignaturePolicyConfig {
		return config.SignaturePolicyConfig{Enabled: true, Action: action, PublicKeys: keys}
	}
}

func newKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	pemBytes, err := cryptoutils.MarshalPublicKeyToPEM(key.Public())
	require.NoError(t, err)
	return key, string(pemBytes)
}

func simpleSigningPayload(subject digest.Digest) []byte {
	return fmt.Appendf(nil, `{"critical":{"identity":{"docker-reference":"harbor/app"},"image":{"docker-manifest-digest":%q},"type":"cosign container image signature"},"optional":null}`, subject)
}

func push(t *testing.T, target *memory.Store, mediaType string, payload []byte) ocispec.Descriptor {
	t.Helper()
	desc := content.NewDescriptorFromBytes(mediaType, payload)
	if err := target.Push(context.Background(), desc, bytes.NewReader(payload)); !errors.Is(err, errdef.ErrAlreadyExists) {
		require.NoError(t, err)
	}
	return desc
}

// pushManifest stores a one-layer manifest and returns its descriptor.
func pushManifest(t *testing.T, target *memory.Store, artifactType string, layer ocispec.Descriptor) ocispec.Descriptor {
	t.Helper()
	config := push(t, target, ocispec.MediaTypeEmptyJSON, ocispec.DescriptorEmptyJSON.Data)
	payload, err := json.Marshal(ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       config,
		Layers:       []ocispec.Descriptor{layer},
	})
	require.NoError(t, err)
	desc := push(t, target, ocispec.MediaTypeImageManifest, payload)
	desc.ArtifactType = artifactType
	return desc
}

// legacySignatureFor stores a cosign tag-scheme signature manifest.
func legacySignatureFor(t *testing.T, target *memory.Store, payload []byte, annotations map[string]string) ocispec.Descriptor {
	t.Helper()
	layer := push(t, target, mediaTypeSimpleSigning, payload)
	layer.Annotations = annotations
	return pushManifest(t, target, "", layer)
}

func keySignedLegacy(t *testing.T, target *memory.Store, key *ecdsa.PrivateKey, subject digest.Digest) ocispec.Descriptor {
	t.Helper()
	payload := simpleSigningPayload(subject)
	sum := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, key, sum[:])
	require.NoError(t, err)
	return legacySignatureFor(t, target, payload, map[string]string{annotationSignature: base64.StdEncoding.EncodeToString(sig)})
}

func cosignStatement(t *testing.T, predicateType string, subject digest.Digest) []byte {
	t.Helper()
	statement, err := json.Marshal(map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"subject":       []map[string]any{{"name": "harbor/app", "digest": map[string]string{"sha256": subject.Encoded()}}},
		"predicateType": predicateType,
		"predicate":     map[string]any{},
	})
	require.NoError(t, err)
	return statement
}

// keyBundleFor signs statement with a fresh key into a sigstore bundle
// referrer and returns the referrer and the key's PEM.
func keyBundleFor(t *testing.T, target *memory.Store, statement []byte) (ocispec.Descriptor, string) {
	t.Helper()
	keypair, err := sign.NewEphemeralKeypair(nil)
	require.NoError(t, err)
	pemKey, err := keypair.GetPublicKeyPem()
	require.NoError(t, err)
	signed, err := sign.Bundle(&sign.DSSEData{Data: statement, PayloadType: testInTotoType}, keypair, sign.BundleOptions{})
	require.NoError(t, err)
	payload, err := protojson.Marshal(signed)
	require.NoError(t, err)
	layer := push(t, target, testBundleMedia, payload)
	return pushManifest(t, target, testBundleMedia, layer), pemKey
}

func TestDisabledPolicyAcceptsUnsignedImage(t *testing.T) {
	verifier := NewVerifier(func() config.SignaturePolicyConfig { return config.SignaturePolicyConfig{} }, t.TempDir())
	require.NoError(t, verifier.Verify(context.Background(), "harbor/app:1", store.Signatures{Subject: testSubject}))
}

func TestKeyVerifiesTagSchemeSignature(t *testing.T) {
	target := memory.New()
	key, pemKey := newKey(t)
	sig := keySignedLegacy(t, target, key, testSubject.Digest)
	signatures := store.Signatures{Fetcher: target, Subject: testSubject, Tagged: []ocispec.Descriptor{sig}}

	verifier := NewVerifier(policyWithKeys(config.SignatureActionBlock, pemKey), t.TempDir())
	require.NoError(t, verifier.Verify(context.Background(), "harbor/app:1", signatures))

	keyFile := t.TempDir() + "/cosign.pub"
	require.NoError(t, os.WriteFile(keyFile, []byte(pemKey), 0o600))
	fromFile := NewVerifier(policyWithKeys(config.SignatureActionBlock, keyFile), t.TempDir())
	require.NoError(t, fromFile.Verify(context.Background(), "harbor/app:1", signatures))
}

func TestBlockRejectsUnverifiedImage(t *testing.T) {
	target := memory.New()
	key, signerKey := newKey(t)
	_, otherKey := newKey(t)
	signed := keySignedLegacy(t, target, key, testSubject.Digest)
	signedForOther := keySignedLegacy(t, target, key, digest.FromString("another image"))

	tests := []struct {
		name   string
		key    string
		tagged []ocispec.Descriptor
		want   string
	}{
		{name: "unsigned", key: signerKey, want: "no cosign signature found"},
		{name: "other key", key: otherKey, tagged: []ocispec.Descriptor{signed}, want: "public_keys[0]"},
		{name: "signature for another digest", key: signerKey, tagged: []ocispec.Descriptor{signedForOther}, want: "not " + testSubject.Digest.String()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signatures := store.Signatures{Fetcher: target, Subject: testSubject, Tagged: test.tagged}
			err := NewVerifier(policyWithKeys(config.SignatureActionBlock, test.key), t.TempDir()).
				Verify(context.Background(), "harbor/app:1", signatures)
			require.ErrorIs(t, err, store.ErrSignatureRejected)
			require.ErrorContains(t, err, test.want)

			warn := NewVerifier(policyWithKeys(config.SignatureActionWarn, test.key), t.TempDir())
			require.NoError(t, warn.Verify(context.Background(), "harbor/app:1", signatures))
		})
	}
}

func TestKeyVerifiesBundleReferrer(t *testing.T) {
	target := memory.New()
	referrer, pemKey := keyBundleFor(t, target, cosignStatement(t, cosignSignPredicateType, testSubject.Digest))
	signatures := store.Signatures{Fetcher: target, Subject: testSubject, Referrers: []ocispec.Descriptor{referrer}}

	require.NoError(t, NewVerifier(policyWithKeys("", pemKey), t.TempDir()).Verify(context.Background(), "harbor/app:1", signatures))

	other := testSubject
	other.Digest = digest.FromString("another image")
	signatures.Subject = other
	err := NewVerifier(policyWithKeys("", pemKey), t.TempDir()).Verify(context.Background(), "harbor/app:2", signatures)
	require.ErrorIs(t, err, store.ErrSignatureRejected)
}

func TestAttestationIsNotAnImageSignature(t *testing.T) {
	target := memory.New()
	referrer, pemKey := keyBundleFor(t, target, cosignStatement(t, "https://spdx.dev/Document", testSubject.Digest))
	signatures := store.Signatures{Fetcher: target, Subject: testSubject, Referrers: []ocispec.Descriptor{referrer}}

	err := NewVerifier(policyWithKeys("", pemKey), t.TempDir()).Verify(context.Background(), "harbor/app:1", signatures)
	require.ErrorIs(t, err, store.ErrSignatureRejected)
	require.ErrorContains(t, err, "attestation")
}

func TestReferrersWithoutBundlesAreIgnored(t *testing.T) {
	target := memory.New()
	_, pemKey := newKey(t)
	sbom := pushManifest(t, target, "application/spdx+json", push(t, target, "application/spdx+json", []byte(`{}`)))
	signatures := store.Signatures{Fetcher: target, Subject: testSubject, Referrers: []ocispec.Descriptor{sbom}}

	err := NewVerifier(policyWithKeys("", pemKey), t.TempDir()).Verify(context.Background(), "harbor/app:1", signatures)
	require.ErrorContains(t, err, "no cosign signature found")
}

func keylessVerifier(t *testing.T, sigstore *ca.VirtualSigstore, subject string) *Verifier {
	t.Helper()
	verifier := NewVerifier(func() config.SignaturePolicyConfig {
		return config.SignaturePolicyConfig{Enabled: true, Keyless: config.KeylessSignatureConfig{
			Enabled: true, Issuer: testIssuer, Subject: subject,
		}}
	}, t.TempDir())
	verifier.loadRoot = func(config.KeylessSignatureConfig) (root.TrustedMaterial, error) { return sigstore, nil }
	// The virtual CA issues certificates without embedded SCTs.
	verifier.keylessOptions = []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1)}
	return verifier
}

func TestKeylessVerifiesTagSchemeSignature(t *testing.T) {
	sigstore, err := ca.NewVirtualSigstore()
	require.NoError(t, err)
	payload := simpleSigningPayload(testSubject.Digest)
	entity, err := sigstore.Sign(testIdentity, testIssuer, payload)
	require.NoError(t, err)
	annotations := legacyKeylessAnnotations(t, sigstore, entity)
	target := memory.New()
	sig := legacySignatureFor(t, target, payload, annotations)
	signatures := store.Signatures{Fetcher: target, Subject: testSubject, Tagged: []ocispec.Descriptor{sig}}

	require.NoError(t, keylessVerifier(t, sigstore, testIdentity).Verify(context.Background(), "harbor/app:1", signatures))

	err = keylessVerifier(t, sigstore, "someone-else@example.com").Verify(context.Background(), "harbor/app:1", signatures)
	require.ErrorIs(t, err, store.ErrSignatureRejected)

	delete(annotations, annotationRekorBundle)
	noTlog := legacySignatureFor(t, target, payload, annotations)
	signatures.Tagged = []ocispec.Descriptor{noTlog}
	err = keylessVerifier(t, sigstore, testIdentity).Verify(context.Background(), "harbor/app:1", signatures)
	require.ErrorIs(t, err, store.ErrSignatureRejected, "a keyless signature needs its transparency log entry")
}

func TestKeylessVerifiesBundle(t *testing.T) {
	sigstore, err := ca.NewVirtualSigstore()
	require.NoError(t, err)
	entity, err := sigstore.Attest(testIdentity, testIssuer, cosignStatement(t, cosignSignPredicateType, testSubject.Digest))
	require.NoError(t, err)
	verifier := keylessVerifier(t, sigstore, testIdentity)
	checkers, err := verifier.checkersFor(verifier.policy())
	require.NoError(t, err)
	candidate := &bundleSignature{manifest: digest.FromString("bundle"), entity: entity}

	require.NoError(t, candidate.verify(checkers[0], testSubject))
	other := testSubject
	other.Digest = digest.FromString("another image")
	require.Error(t, candidate.verify(checkers[0], other))
}

// legacyKeylessAnnotations lays out a keyless signature the way cosign
// stores it on a tag-scheme signature layer.
func legacyKeylessAnnotations(t *testing.T, sigstore *ca.VirtualSigstore, entity *ca.TestEntity) map[string]string {
	t.Helper()
	verification, err := entity.VerificationContent()
	require.NoError(t, err)
	certPEM, err := cryptoutils.MarshalCertificateToPEM(verification.Certificate())
	require.NoError(t, err)
	signatureContent, err := entity.SignatureContent()
	require.NoError(t, err)
	entries, err := entity.TlogEntries()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	tle := entries[0].TransparencyLogEntry()
	logID := hex.EncodeToString(tle.GetLogId().GetKeyId())
	set, err := sigstore.RekorSignPayload(tlog.RekorPayload{
		Body:           base64.StdEncoding.EncodeToString(tle.GetCanonicalizedBody()),
		IntegratedTime: tle.GetIntegratedTime(),
		LogIndex:       tle.GetLogIndex(),
		LogID:          logID,
	})
	require.NoError(t, err)
	rekorBundle, err := json.Marshal(map[string]any{
		"SignedEntryTimestamp": set,
		"Payload": map[string]any{
			"body":           tle.GetCanonicalizedBody(),
			"integratedTime": tle.GetIntegratedTime(),
			"logIndex":       tle.GetLogIndex(),
			"logID":          logID,
		},
	})
	require.NoError(t, err)
	return map[string]string{
		annotationSignature:   base64.StdEncoding.EncodeToString(signatureContent.Signature()),
		annotationCertificate: string(certPEM),
		annotationRekorBundle: string(rekorBundle),
	}
}

func TestUnreadableSignatureDoesNotHideAValidOne(t *testing.T) {
	target := memory.New()
	key, pemKey := newKey(t)
	payload := simpleSigningPayload(testSubject.Digest)
	sum := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, key, sum[:])
	require.NoError(t, err)
	broken := push(t, target, mediaTypeSimpleSigning, []byte(`{"critical":{}}`))
	broken.Annotations = map[string]string{annotationSignature: "c2ln", annotationRekorBundle: `{"Payload":{"body":"e30="}}`}
	good := push(t, target, mediaTypeSimpleSigning, payload)
	good.Annotations = map[string]string{annotationSignature: base64.StdEncoding.EncodeToString(sig)}
	manifestPayload, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    push(t, target, ocispec.MediaTypeEmptyJSON, ocispec.DescriptorEmptyJSON.Data),
		Layers:    []ocispec.Descriptor{broken, good},
	})
	require.NoError(t, err)
	sigManifest := push(t, target, ocispec.MediaTypeImageManifest, manifestPayload)
	signatures := store.Signatures{Fetcher: target, Subject: testSubject, Tagged: []ocispec.Descriptor{sigManifest}}

	require.NoError(t, NewVerifier(policyWithKeys("", pemKey), t.TempDir()).Verify(context.Background(), "harbor/app:1", signatures))
}
