package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func generateTestCert(t *testing.T, notBefore, notAfter time.Time) (certPEM, keyPEM []byte) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
			CommonName:   "test.example.com",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"test.example.com", "localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM
}

func writeTempFiles(t *testing.T, certPEM, keyPEM []byte) (certPath, keyPath string) {
	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))

	return certPath, keyPath
}

func TestLoadCertificate(t *testing.T) {
	t.Run("loads valid certificate", func(t *testing.T) {
		certPEM, keyPEM := generateTestCert(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		certPath, keyPath := writeTempFiles(t, certPEM, keyPEM)

		cert, err := LoadCertificate(certPath, keyPath)
		require.NoError(t, err)
		require.NotNil(t, cert)
	})

	t.Run("returns error for missing cert file", func(t *testing.T) {
		_, err := LoadCertificate("/nonexistent/cert.pem", "/nonexistent/key.pem")
		require.ErrorIs(t, err, ErrCertNotFound)
	})

	t.Run("returns error for missing key file", func(t *testing.T) {
		certPEM, _ := generateTestCert(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		dir := t.TempDir()
		certPath := filepath.Join(dir, "cert.pem")
		require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))

		_, err := LoadCertificate(certPath, "/nonexistent/key.pem")
		require.ErrorIs(t, err, ErrKeyNotFound)
	})

	t.Run("returns error for invalid certificate", func(t *testing.T) {
		dir := t.TempDir()
		certPath := filepath.Join(dir, "cert.pem")
		keyPath := filepath.Join(dir, "key.pem")

		require.NoError(t, os.WriteFile(certPath, []byte("invalid cert"), 0o600))
		require.NoError(t, os.WriteFile(keyPath, []byte("invalid key"), 0o600))

		_, err := LoadCertificate(certPath, keyPath)
		require.ErrorIs(t, err, ErrCertInvalid)
	})
}

func TestValidateCertificate(t *testing.T) {
	t.Run("validates valid certificate", func(t *testing.T) {
		certPEM, _ := generateTestCert(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		dir := t.TempDir()
		certPath := filepath.Join(dir, "cert.pem")
		require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))

		err := ValidateCertificate(certPath)
		require.NoError(t, err)
	})

	t.Run("rejects expired certificate", func(t *testing.T) {
		certPEM, _ := generateTestCert(t, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
		dir := t.TempDir()
		certPath := filepath.Join(dir, "cert.pem")
		require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))

		err := ValidateCertificate(certPath)
		require.ErrorIs(t, err, ErrCertExpired)
	})

	t.Run("rejects not-yet-valid certificate", func(t *testing.T) {
		certPEM, _ := generateTestCert(t, time.Now().Add(time.Hour), time.Now().Add(2*time.Hour))
		dir := t.TempDir()
		certPath := filepath.Join(dir, "cert.pem")
		require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))

		err := ValidateCertificate(certPath)
		require.ErrorIs(t, err, ErrCertNotYetValid)
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		err := ValidateCertificate("/nonexistent/cert.pem")
		require.ErrorIs(t, err, ErrCertNotFound)
	})
}

func TestLoadCAPool(t *testing.T) {
	t.Run("loads valid CA", func(t *testing.T) {
		certPEM, _ := generateTestCert(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		dir := t.TempDir()
		caPath := filepath.Join(dir, "ca.pem")
		require.NoError(t, os.WriteFile(caPath, certPEM, 0o600))

		pool, err := LoadCAPool(caPath)
		require.NoError(t, err)
		require.NotNil(t, pool)
	})

	t.Run("returns error for missing CA file", func(t *testing.T) {
		_, err := LoadCAPool("/nonexistent/ca.pem")
		require.ErrorIs(t, err, ErrCANotFound)
	})

	t.Run("returns error for invalid CA", func(t *testing.T) {
		dir := t.TempDir()
		caPath := filepath.Join(dir, "ca.pem")
		require.NoError(t, os.WriteFile(caPath, []byte("not a cert"), 0o600))

		_, err := LoadCAPool(caPath)
		require.ErrorIs(t, err, ErrCAInvalid)
	})
}

func TestLoadClientTLSConfig(t *testing.T) {
	t.Run("loads config without client cert", func(t *testing.T) {
		cfg := DefaultConfig()
		tlsCfg, err := LoadClientTLSConfig(cfg)
		require.NoError(t, err)
		require.NotNil(t, tlsCfg)
		require.Empty(t, tlsCfg.Certificates)
	})

	t.Run("loads config with client cert (mTLS)", func(t *testing.T) {
		certPEM, keyPEM := generateTestCert(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		certPath, keyPath := writeTempFiles(t, certPEM, keyPEM)

		cfg := DefaultConfig()
		cfg.CertFile = certPath
		cfg.KeyFile = keyPath

		tlsCfg, err := LoadClientTLSConfig(cfg)
		require.NoError(t, err)
		require.Len(t, tlsCfg.Certificates, 1)
	})

	t.Run("loads config with CA", func(t *testing.T) {
		certPEM, _ := generateTestCert(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		dir := t.TempDir()
		caPath := filepath.Join(dir, "ca.pem")
		require.NoError(t, os.WriteFile(caPath, certPEM, 0o600))

		cfg := DefaultConfig()
		cfg.CAFile = caPath

		tlsCfg, err := LoadClientTLSConfig(cfg)
		require.NoError(t, err)
		require.NotNil(t, tlsCfg.RootCAs)
	})
}

func TestLoadServerTLSConfig(t *testing.T) {
	t.Run("loads server config", func(t *testing.T) {
		certPEM, keyPEM := generateTestCert(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		certPath, keyPath := writeTempFiles(t, certPEM, keyPEM)

		cfg := DefaultConfig()
		cfg.CertFile = certPath
		cfg.KeyFile = keyPath

		tlsCfg, err := LoadServerTLSConfig(cfg)
		require.NoError(t, err)
		require.Len(t, tlsCfg.Certificates, 1)
	})

	t.Run("returns error without cert", func(t *testing.T) {
		cfg := DefaultConfig()
		_, err := LoadServerTLSConfig(cfg)
		require.ErrorIs(t, err, ErrCertNotFound)
	})

	t.Run("loads server config with client CA (mTLS)", func(t *testing.T) {
		certPEM, keyPEM := generateTestCert(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		certPath, keyPath := writeTempFiles(t, certPEM, keyPEM)

		dir := t.TempDir()
		caPath := filepath.Join(dir, "ca.pem")
		require.NoError(t, os.WriteFile(caPath, certPEM, 0o600))

		cfg := DefaultConfig()
		cfg.CertFile = certPath
		cfg.KeyFile = keyPath
		cfg.CAFile = caPath

		tlsCfg, err := LoadServerTLSConfig(cfg)
		require.NoError(t, err)
		require.NotNil(t, tlsCfg.ClientCAs)
	})
}

func TestGetCertificateInfo(t *testing.T) {
	t.Run("extracts certificate info", func(t *testing.T) {
		notBefore := time.Now().Add(-time.Hour)
		notAfter := time.Now().Add(24 * time.Hour)
		certPEM, _ := generateTestCert(t, notBefore, notAfter)

		dir := t.TempDir()
		certPath := filepath.Join(dir, "cert.pem")
		require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))

		info, err := GetCertificateInfo(certPath)
		require.NoError(t, err)
		require.Contains(t, info.Subject, "Test Org")
		require.False(t, info.IsExpired)
		require.True(t, info.DaysToExpiry >= 0)
		require.Contains(t, info.DNSNames, "test.example.com")
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.Equal(t, uint16(0x0303), cfg.MinVersion) // TLS 1.2
	require.False(t, cfg.SkipVerify)
}
