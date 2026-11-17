package signature

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	rekorpb "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"oras.land/oras-go/v2/content"
)

const (
	// Media type and annotations of a cosign tag-scheme signature layer.
	mediaTypeSimpleSigning   = "application/vnd.dev.cosign.simplesigning.v1+json"
	annotationSignature      = "dev.cosignproject.cosign/signature"
	annotationCertificate    = "dev.sigstore.cosign/certificate"
	annotationRekorBundle    = "dev.sigstore.cosign/bundle"
	cosignSignatureType      = "cosign container image signature"
	mediaTypeBundlePrefix    = "application/vnd.dev.sigstore.bundle"
	cosignSignPredicateType  = "https://sigstore.dev/cosign/sign/v1"
	maxSignatureContentBytes = 1 << 20
)

// candidate is one signature that may satisfy the policy.
type candidate interface {
	name() string
	verify(check checker, subject ocispec.Descriptor) error
}

// loadCandidates reads every cosign signature of the subject: each layer of
// the tag-scheme .sig manifests and each sigstore bundle referrer.
// Attestations and other referrers are ignored. A signature that cannot be
// read is reported in the returned errors and does not hide the others.
func loadCandidates(ctx context.Context, signatures store.Signatures) ([]candidate, []error) {
	var candidates []candidate
	var failures []error
	for _, desc := range signatures.Tagged {
		legacy, err := loadLegacy(ctx, signatures.Fetcher, desc)
		candidates = append(candidates, legacy...)
		failures = append(failures, err...)
	}
	for _, desc := range signatures.Referrers {
		if !strings.HasPrefix(desc.ArtifactType, mediaTypeBundlePrefix) {
			continue
		}
		referrer, err := loadBundle(ctx, signatures.Fetcher, desc)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		candidates = append(candidates, referrer)
	}
	return candidates, failures
}

func fetchManifest(ctx context.Context, fetcher content.Fetcher, desc ocispec.Descriptor) (ocispec.Manifest, error) {
	payload, err := fetchBounded(ctx, fetcher, desc)
	if err != nil {
		return ocispec.Manifest{}, err
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return ocispec.Manifest{}, fmt.Errorf("decode signature manifest %s: %w", desc.Digest, err)
	}
	return manifest, nil
}

func fetchBounded(ctx context.Context, fetcher content.Fetcher, desc ocispec.Descriptor) ([]byte, error) {
	if desc.Size > maxSignatureContentBytes {
		return nil, fmt.Errorf("signature content %s is larger than %d bytes", desc.Digest, maxSignatureContentBytes)
	}
	payload, err := content.FetchAll(ctx, fetcher, desc)
	if err != nil {
		return nil, fmt.Errorf("read signature content %s: %w", desc.Digest, err)
	}
	return payload, nil
}

// legacySignature is one layer of a cosign tag-scheme signature manifest.
type legacySignature struct {
	manifest  digest.Digest
	payload   []byte
	signature []byte
	cert      *x509.Certificate
	tlogEntry *tlog.Entry
}

// loadLegacy reads each signature layer of a tag-scheme manifest. One .sig
// manifest collects every signature of the digest, so a layer that cannot be
// read is reported and the other layers are still returned.
func loadLegacy(ctx context.Context, fetcher content.Fetcher, desc ocispec.Descriptor) ([]candidate, []error) {
	manifest, err := fetchManifest(ctx, fetcher, desc)
	if err != nil {
		return nil, []error{err}
	}
	var candidates []candidate
	var failures []error
	for _, layer := range manifest.Layers {
		if layer.MediaType != mediaTypeSimpleSigning {
			continue
		}
		payload, err := fetchBounded(ctx, fetcher, layer)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		legacy, err := newLegacySignature(desc.Digest, payload, layer.Annotations)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		candidates = append(candidates, legacy)
	}
	return candidates, failures
}

func newLegacySignature(manifest digest.Digest, payload []byte, annotations map[string]string) (*legacySignature, error) {
	sig, err := base64.StdEncoding.DecodeString(annotations[annotationSignature])
	if err != nil {
		return nil, fmt.Errorf("decode signature in %s: %w", manifest, err)
	}
	legacy := &legacySignature{manifest: manifest, payload: payload, signature: sig}
	if certPEM := annotations[annotationCertificate]; certPEM != "" {
		certs, err := cryptoutils.UnmarshalCertificatesFromPEM([]byte(certPEM))
		if err != nil || len(certs) == 0 {
			return nil, fmt.Errorf("decode certificate in %s: %w", manifest, errors.Join(err, errors.New("no certificate")))
		}
		legacy.cert = certs[0]
	}
	if rekorBundle := annotations[annotationRekorBundle]; rekorBundle != "" {
		legacy.tlogEntry, err = tlogEntryFromRekorBundle([]byte(rekorBundle))
		if err != nil {
			return nil, fmt.Errorf("decode transparency log entry in %s: %w", manifest, err)
		}
	}
	return legacy, nil
}

// rekorBundle is the dev.sigstore.cosign/bundle annotation: a Rekor v1 entry
// with its signed entry timestamp.
type rekorBundle struct {
	SignedEntryTimestamp []byte `json:"SignedEntryTimestamp"` //nolint:tagliatelle // cosign's field name
	Payload              struct {
		Body           []byte `json:"body"`
		IntegratedTime int64  `json:"integratedTime"`
		LogIndex       int64  `json:"logIndex"`
		LogID          string `json:"logID"` //nolint:tagliatelle // cosign's field name
	} `json:"Payload"` //nolint:tagliatelle // cosign's field name
}

func tlogEntryFromRekorBundle(annotation []byte) (*tlog.Entry, error) {
	var parsed rekorBundle
	if err := json.Unmarshal(annotation, &parsed); err != nil {
		return nil, err
	}
	logID, err := hex.DecodeString(parsed.Payload.LogID)
	if err != nil {
		return nil, fmt.Errorf("decode log ID: %w", err)
	}
	var kind struct {
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
	}
	if err := json.Unmarshal(parsed.Payload.Body, &kind); err != nil {
		return nil, fmt.Errorf("decode entry body: %w", err)
	}
	return tlog.NewTlogEntry(&rekorpb.TransparencyLogEntry{
		LogIndex:          parsed.Payload.LogIndex,
		LogId:             &protocommon.LogId{KeyId: logID},
		KindVersion:       &rekorpb.KindVersion{Kind: kind.Kind, Version: kind.APIVersion},
		IntegratedTime:    parsed.Payload.IntegratedTime,
		InclusionPromise:  &rekorpb.InclusionPromise{SignedEntryTimestamp: parsed.SignedEntryTimestamp},
		CanonicalizedBody: parsed.Payload.Body,
	})
}

func (l *legacySignature) name() string {
	return "signature " + l.manifest.String()
}

func (l *legacySignature) verify(check checker, subject ocispec.Descriptor) error {
	entity := &legacyEntity{signature: l}
	if check.keyless {
		if l.cert == nil {
			return errors.New("signature has no certificate")
		}
		entity.keyless = true
	}
	policy := verify.NewPolicy(verify.WithArtifact(bytes.NewReader(l.payload)), check.identity)
	if _, err := check.verifier.Verify(entity, policy); err != nil {
		return err
	}
	return checkSimpleSigningPayload(l.payload, subject.Digest)
}

// checkSimpleSigningPayload binds a verified payload to the subject digest.
func checkSimpleSigningPayload(payload []byte, subject digest.Digest) error {
	var simple struct {
		Critical struct {
			Image struct {
				DockerManifestDigest string `json:"docker-manifest-digest"`
			} `json:"image"`
			Type string `json:"type"`
		} `json:"critical"`
	}
	if err := json.Unmarshal(payload, &simple); err != nil {
		return fmt.Errorf("decode signed payload: %w", err)
	}
	if simple.Critical.Type != cosignSignatureType {
		return fmt.Errorf("signed payload type %q is not a cosign image signature", simple.Critical.Type)
	}
	if simple.Critical.Image.DockerManifestDigest != subject.String() {
		return fmt.Errorf("signed payload names digest %s, not %s", simple.Critical.Image.DockerManifestDigest, subject)
	}
	return nil
}

// legacyEntity presents a tag-scheme signature to sigstore-go. Key checks see
// only the signature. Keyless checks also see the certificate and the Rekor
// entry with its inclusion promise.
type legacyEntity struct {
	verify.BaseSignedEntity
	signature *legacySignature
	keyless   bool
}

func (e *legacyEntity) HasInclusionPromise() bool {
	return e.keyless && e.signature.tlogEntry != nil
}

func (e *legacyEntity) SignatureContent() (verify.SignatureContent, error) {
	sum := sha256.Sum256(e.signature.payload)
	return bundle.NewMessageSignature(sum[:], "SHA2_256", e.signature.signature), nil
}

func (e *legacyEntity) VerificationContent() (verify.VerificationContent, error) {
	if e.keyless {
		return bundle.NewCertificate(e.signature.cert), nil
	}
	return &bundle.PublicKey{}, nil
}

func (e *legacyEntity) Timestamps() ([][]byte, error) {
	return nil, nil
}

func (e *legacyEntity) TlogEntries() ([]*tlog.Entry, error) {
	if !e.keyless || e.signature.tlogEntry == nil {
		return nil, nil
	}
	return []*tlog.Entry{e.signature.tlogEntry}, nil
}

func (e *legacyEntity) Version() (string, error) {
	return "v0.1", nil
}

// bundleSignature is a sigstore bundle attached as an OCI referrer, as
// cosign sign writes it with the new bundle format.
type bundleSignature struct {
	manifest digest.Digest
	entity   verify.SignedEntity
}

func loadBundle(ctx context.Context, fetcher content.Fetcher, desc ocispec.Descriptor) (candidate, error) {
	manifest, err := fetchManifest(ctx, fetcher, desc)
	if err != nil {
		return nil, err
	}
	for _, layer := range manifest.Layers {
		if !strings.HasPrefix(layer.MediaType, mediaTypeBundlePrefix) {
			continue
		}
		payload, err := fetchBounded(ctx, fetcher, layer)
		if err != nil {
			return nil, err
		}
		var parsed bundle.Bundle
		if err := parsed.UnmarshalJSON(payload); err != nil {
			return nil, fmt.Errorf("decode sigstore bundle %s: %w", desc.Digest, err)
		}
		return &bundleSignature{manifest: desc.Digest, entity: &parsed}, nil
	}
	return nil, fmt.Errorf("referrer %s has no sigstore bundle layer", desc.Digest)
}

func (b *bundleSignature) name() string {
	return "bundle " + b.manifest.String()
}

func (b *bundleSignature) verify(check checker, subject ocispec.Descriptor) error {
	if err := requireSignaturePredicate(b.entity); err != nil {
		return err
	}
	subjectDigest, err := hex.DecodeString(subject.Digest.Encoded())
	if err != nil {
		return fmt.Errorf("decode subject digest: %w", err)
	}
	policy := verify.NewPolicy(verify.WithArtifactDigest(subject.Digest.Algorithm().String(), subjectDigest), check.identity)
	_, err = check.verifier.Verify(b.entity, policy)
	return err
}

// requireSignaturePredicate rejects attestations. An SBOM attestation is
// signed by the same key, but it does not say the image itself was signed.
func requireSignaturePredicate(signed verify.SignatureProvider) error {
	content, err := signed.SignatureContent()
	if err != nil {
		return err
	}
	envelope := content.EnvelopeContent()
	if envelope == nil {
		return nil
	}
	statement, err := envelope.Statement()
	if err != nil {
		return err
	}
	if statement.GetPredicateType() != cosignSignPredicateType {
		return fmt.Errorf("bundle carries a %s attestation, not a cosign signature", statement.GetPredicateType())
	}
	return nil
}
