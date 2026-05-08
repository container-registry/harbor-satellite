package policy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/require"
)

// generateKey creates a P-256 ECDSA key pair and writes the public key to a PEM
// file at path. Returns the private key for signing test payloads.
func generateKey(t *testing.T, path string) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	require.NoError(t, pem.Encode(f, &pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	return key
}

// signPayload returns a DER-encoded ECDSA signature over sha256(payload).
func signPayload(t *testing.T, key *ecdsa.PrivateKey, payload []byte) []byte {
	t.Helper()
	hash := sha256.Sum256(payload)
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	require.NoError(t, err)
	sig, err := asn1.Marshal(struct{ R, S *big.Int }{r, s})
	require.NoError(t, err)
	return sig
}

// simpleSigning builds the cosign simple-signing payload JSON for the given digest.
func simpleSigning(digest v1.Hash, ref string) []byte {
	type criticalImage struct {
		DockerManifestDigest string `json:"docker-manifest-digest"`
	}
	type criticalIdentity struct {
		DockerReference string `json:"docker-reference"`
	}
	type critical struct {
		Identity criticalIdentity `json:"identity"`
		Image    criticalImage    `json:"image"`
		Type     string           `json:"type"`
	}
	payload, _ := json.Marshal(struct {
		Critical critical `json:"critical"`
	}{
		Critical: critical{
			Identity: criticalIdentity{DockerReference: ref},
			Image:    criticalImage{DockerManifestDigest: digest.String()},
			Type:     "cosign container image signature",
		},
	})
	return payload
}

// newTestRegistry starts an in-process OCI registry and returns the server
// and its host:port address.
func newTestRegistry(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	return srv, strings.TrimPrefix(srv.URL, "http://")
}

// pushSignedImage pushes a random image to addr/repo/name:tag and then pushes a
// cosign signature artifact for it signed with key. Returns the image digest.
func pushSignedImage(t *testing.T, key *ecdsa.PrivateKey, addr, repo, imgName, tag string) v1.Hash {
	t.Helper()

	img, err := random.Image(512, 1)
	require.NoError(t, err)

	imgRef, err := name.ParseReference(fmt.Sprintf("%s/%s/%s:%s", addr, repo, imgName, tag), name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(imgRef, img))

	digest, err := img.Digest()
	require.NoError(t, err)

	refStr := fmt.Sprintf("%s/%s/%s:%s", addr, repo, imgName, tag)
	payload := simpleSigning(digest, refStr)
	sigDER := signPayload(t, key, payload)

	sigLayer := static.NewLayer(payload, "application/vnd.dev.cosign.simplesigning.v1+json")
	sigLayerHash, err := sigLayer.Digest()
	require.NoError(t, err)

	sigImg, err := mutate.AppendLayers(emptyImage(), sigLayer)
	require.NoError(t, err)

	sigImg = mutate.Annotations(sigImg, map[string]string{}).(v1.Image)
	sigImg, err = setLayerAnnotation(sigImg, sigLayerHash, cosignSigAnnotation, base64.StdEncoding.EncodeToString(sigDER))
	require.NoError(t, err)

	sigTag := fmt.Sprintf("sha256-%s.sig", digest.Hex)
	sigRef, err := name.ParseReference(
		fmt.Sprintf("%s/%s/%s:%s", addr, repo, imgName, sigTag),
		name.Insecure,
	)
	require.NoError(t, err)
	require.NoError(t, remote.Write(sigRef, sigImg))

	return digest
}

// emptyImage returns a minimal OCI image with no layers.
func emptyImage() v1.Image {
	base, err := random.Image(0, 0)
	if err != nil {
		panic(err)
	}
	img, _ := mutate.AppendLayers(
		mutate.MediaType(
			mutate.ConfigMediaType(base, types.OCIContentDescriptor),
			types.OCIManifestSchema1,
		),
	)
	return img
}

// setLayerAnnotation rebuilds the manifest with an annotation on the descriptor
// for the layer identified by hash.
func setLayerAnnotation(img v1.Image, hash v1.Hash, key, val string) (v1.Image, error) {
	manifest, err := img.Manifest()
	if err != nil {
		return nil, err
	}
	for i, l := range manifest.Layers {
		if l.Digest == hash {
			if manifest.Layers[i].Annotations == nil {
				manifest.Layers[i].Annotations = map[string]string{}
			}
			manifest.Layers[i].Annotations[key] = val
		}
	}
	return &annotatedImage{Image: img, manifest: manifest}, nil
}

// annotatedImage wraps a v1.Image to return a modified manifest.
// Both Manifest() and RawManifest() are overridden because remote.Write
// serialises via RawManifest() when pushing to a registry.
type annotatedImage struct {
	v1.Image
	manifest *v1.Manifest
}

func (a *annotatedImage) Manifest() (*v1.Manifest, error) { return a.manifest, nil }

func (a *annotatedImage) RawManifest() ([]byte, error) {
	return json.Marshal(a.manifest)
}

// --- tests ---

func TestNew_Disabled(t *testing.T) {
	v, err := New(Config{Enabled: false})
	require.NoError(t, err)
	require.NoError(t, v.Verify(context.Background(), "reg/img:tag", false, "", ""))
}

func TestNew_EnabledNoKeys(t *testing.T) {
	_, err := New(Config{Enabled: true})
	require.ErrorContains(t, err, "public_key")
}

func TestNew_InvalidAction(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pem")
	generateKey(t, keyPath)

	_, err := New(Config{Enabled: true, PublicKeys: []string{keyPath}, Action: "invalid"})
	require.ErrorContains(t, err, "invalid action")
}

func TestNew_DefaultsToBlock(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pem")
	generateKey(t, keyPath)

	v, err := New(Config{Enabled: true, PublicKeys: []string{keyPath}})
	require.NoError(t, err)
	cv, ok := v.(*cosignVerifier)
	require.True(t, ok)
	require.Equal(t, ActionBlock, cv.cfg.Action)
}

func TestNew_MissingKeyFile(t *testing.T) {
	_, err := New(Config{Enabled: true, PublicKeys: []string{"/nonexistent/key.pem"}})
	require.ErrorContains(t, err, "read key file")
}

func TestNew_NonPEMKeyFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(keyPath, []byte("not a pem"), 0o600))

	_, err := New(Config{Enabled: true, PublicKeys: []string{keyPath}})
	require.ErrorContains(t, err, "no PEM block")
}

func TestWarnError(t *testing.T) {
	inner := fmt.Errorf("no sigs")
	w := &WarnError{Ref: "docker.io/library/alpine:latest", cause: inner}

	require.True(t, IsWarnError(w))
	require.False(t, IsWarnError(fmt.Errorf("plain error")))
	require.Contains(t, w.Error(), "alpine")
	require.ErrorIs(t, w, inner)
}

func TestVerify_ValidSignature(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "cosign.pub")
	privKey := generateKey(t, keyPath)

	_, addr := newTestRegistry(t)
	pushSignedImage(t, privKey, addr, "library", "signed", "latest")

	v, err := New(Config{Enabled: true, PublicKeys: []string{keyPath}})
	require.NoError(t, err)

	err = v.Verify(context.Background(), addr+"/library/signed:latest", true, "", "")
	require.NoError(t, err)
}

func TestVerify_WrongKey(t *testing.T) {
	dir := t.TempDir()
	signingKeyPath := filepath.Join(dir, "signing.pub")
	signingKey := generateKey(t, signingKeyPath)

	wrongKeyPath := filepath.Join(dir, "wrong.pub")
	generateKey(t, wrongKeyPath) // different key

	_, addr := newTestRegistry(t)
	pushSignedImage(t, signingKey, addr, "library", "signed", "latest")

	v, err := New(Config{Enabled: true, PublicKeys: []string{wrongKeyPath}, Action: ActionBlock})
	require.NoError(t, err)

	err = v.Verify(context.Background(), addr+"/library/signed:latest", true, "", "")
	require.Error(t, err)
	require.False(t, IsWarnError(err))
}

func TestVerify_WarnMode_MissingSignature(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pub")
	generateKey(t, keyPath)

	_, addr := newTestRegistry(t)
	// Push image without any signature artifact
	img, _ := random.Image(512, 1)
	imgRef, _ := name.ParseReference(addr+"/library/unsigned:latest", name.Insecure)
	require.NoError(t, remote.Write(imgRef, img))

	v, err := New(Config{Enabled: true, PublicKeys: []string{keyPath}, Action: ActionWarn})
	require.NoError(t, err)

	err = v.Verify(context.Background(), addr+"/library/unsigned:latest", true, "", "")
	require.Error(t, err)
	require.True(t, IsWarnError(err))
}

func TestVerify_BlockMode_MissingSignature(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pub")
	generateKey(t, keyPath)

	_, addr := newTestRegistry(t)
	img, _ := random.Image(512, 1)
	imgRef, _ := name.ParseReference(addr+"/library/unsigned:latest", name.Insecure)
	require.NoError(t, remote.Write(imgRef, img))

	v, err := New(Config{Enabled: true, PublicKeys: []string{keyPath}, Action: ActionBlock})
	require.NoError(t, err)

	err = v.Verify(context.Background(), addr+"/library/unsigned:latest", true, "", "")
	require.Error(t, err)
	require.False(t, IsWarnError(err))
}
