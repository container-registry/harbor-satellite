# Mock Interfaces for Testing

Interfaces that need to be mocked for unit testing Harbor Satellite zero-trust features.

## Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         MOCK INTERFACES                                      │
│                                                                              │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐        │
│  │ DeviceIdentity  │     │   TPMDevice     │     │  SPIREClient    │        │
│  │   Interface     │     │   Interface     │     │   Interface     │        │
│  └─────────────────┘     └─────────────────┘     └─────────────────┘        │
│                                                                              │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐        │
│  │ GroundControl   │     │  HarborClient   │     │  CryptoProvider │        │
│  │   Interface     │     │   Interface     │     │   Interface     │        │
│  └─────────────────┘     └─────────────────┘     └─────────────────┘        │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 1. DeviceIdentity Interface

Abstracts device hardware identification for fingerprinting.

### Interface Definition

```go
// DeviceIdentity provides device identification for fingerprinting
type DeviceIdentity interface {
    // GetFingerprint returns a unique device fingerprint
    GetFingerprint() (string, error)

    // GetMACAddress returns the primary MAC address
    GetMACAddress() (string, error)

    // GetCPUID returns CPU identifier
    GetCPUID() (string, error)

    // GetBootID returns the current boot ID
    GetBootID() (string, error)

    // GetDiskSerial returns primary disk serial number
    GetDiskSerial() (string, error)

    // GetMachineID returns /etc/machine-id or equivalent
    GetMachineID() (string, error)
}
```

### Mock Implementation

```go
type MockDeviceIdentity struct {
    Fingerprint string
    MACAddress  string
    CPUID       string
    BootID      string
    DiskSerial  string
    MachineID   string
    Err         error
}

func (m *MockDeviceIdentity) GetFingerprint() (string, error) {
    if m.Err != nil {
        return "", m.Err
    }
    return m.Fingerprint, nil
}
// ... other methods
```

### Test Scenarios

- Return consistent fingerprint
- Return different fingerprint when hardware changes
- Handle missing components gracefully
- Error handling when hardware inaccessible

---

## 2. TPMDevice Interface

Abstracts TPM 2.0 operations for hardware attestation.

### Interface Definition

```go
// TPMDevice provides TPM 2.0 operations
type TPMDevice interface {
    // Open initializes connection to TPM
    Open() error

    // Close releases TPM resources
    Close() error

    // GetEK returns the Endorsement Key certificate
    GetEK() ([]byte, error)

    // GetAK creates or retrieves Attestation Key
    GetAK() ([]byte, error)

    // Quote generates a TPM quote over PCRs
    Quote(nonce []byte, pcrs []int) ([]byte, []byte, error)

    // Seal encrypts data bound to PCR state
    Seal(data []byte, pcrs []int) ([]byte, error)

    // Unseal decrypts data if PCR state matches
    Unseal(sealed []byte) ([]byte, error)

    // IsAvailable checks if TPM is present and accessible
    IsAvailable() bool
}
```

### Mock Implementation

```go
type MockTPMDevice struct {
    Available    bool
    EKCert       []byte
    AKPub        []byte
    QuoteData    []byte
    QuoteSig     []byte
    SealedData   map[string][]byte
    Err          error
}

func (m *MockTPMDevice) IsAvailable() bool {
    return m.Available
}

func (m *MockTPMDevice) Quote(nonce []byte, pcrs []int) ([]byte, []byte, error) {
    if m.Err != nil {
        return nil, nil, m.Err
    }
    return m.QuoteData, m.QuoteSig, nil
}
// ... other methods
```

### Test Scenarios

- TPM available and working
- TPM not available (fallback)
- Quote generation and verification
- Seal/unseal roundtrip
- PCR state mismatch on unseal

---

## 3. SPIREClient Interface

Abstracts SPIRE agent Workload API.

### Interface Definition

```go
// SVID represents a SPIFFE Verifiable Identity Document
type SVID struct {
    SPIFFEID    string
    Certificate *x509.Certificate
    PrivateKey  crypto.PrivateKey
    Bundle      []*x509.Certificate
    ExpiresAt   time.Time
}

// SPIREClient provides SPIRE Workload API operations
type SPIREClient interface {
    // Connect establishes connection to SPIRE agent
    Connect(socketPath string) error

    // Close releases connection
    Close() error

    // FetchSVID retrieves current X.509 SVID
    FetchSVID() (*SVID, error)

    // FetchJWTSVID retrieves JWT SVID for audience
    FetchJWTSVID(audience string) (string, error)

    // ValidateSVID validates a peer's SVID
    ValidateSVID(cert *x509.Certificate) error

    // GetTrustBundle retrieves trust bundle
    GetTrustBundle() ([]*x509.Certificate, error)

    // WatchSVID watches for SVID updates
    WatchSVID(ctx context.Context) (<-chan *SVID, error)

    // IsConnected returns connection status
    IsConnected() bool
}
```

### Mock Implementation

```go
type MockSPIREClient struct {
    Connected   bool
    CurrentSVID *SVID
    JWTToken    string
    TrustBundle []*x509.Certificate
    Err         error
    Updates     chan *SVID
}

func (m *MockSPIREClient) FetchSVID() (*SVID, error) {
    if m.Err != nil {
        return nil, m.Err
    }
    return m.CurrentSVID, nil
}

func (m *MockSPIREClient) WatchSVID(ctx context.Context) (<-chan *SVID, error) {
    return m.Updates, nil
}
// ... other methods
```

### Test Scenarios

- Fetch SVID successfully
- SPIRE agent unavailable (fallback)
- SVID expiration and rotation
- Trust bundle updates
- Connection interruption handling

---

## 4. GroundControl Interface

Abstracts Ground Control API operations.

### Interface Definition

```go
// StateConfig contains satellite configuration from Ground Control
type StateConfig struct {
    RegistryURL string
    StateURL    string
    Auth        *RegistryCredentials
}

// GroundControlClient provides Ground Control API operations
type GroundControlClient interface {
    // Register performs zero-touch registration with token
    Register(token string) (*StateConfig, error)

    // RegisterWithSVID performs registration with SVID
    RegisterWithSVID(svid *SVID) (*StateConfig, error)

    // GetState retrieves current satellite state
    GetState() (*SatelliteState, error)

    // GetConfig retrieves satellite configuration
    GetConfig() (*Config, error)

    // Heartbeat sends health status
    Heartbeat(status *HealthStatus) error

    // ValidateToken checks if token is valid
    ValidateToken(token string) (bool, error)

    // SetBaseURL sets the Ground Control URL
    SetBaseURL(url string)
}
```

### Mock Implementation

```go
type MockGroundControlClient struct {
    BaseURL       string
    StateConfig   *StateConfig
    State         *SatelliteState
    Config        *Config
    ValidTokens   map[string]bool
    UsedTokens    map[string]bool
    Err           error
    HeartbeatErr  error
}

func (m *MockGroundControlClient) Register(token string) (*StateConfig, error) {
    if m.Err != nil {
        return nil, m.Err
    }
    if !m.ValidTokens[token] {
        return nil, ErrInvalidToken
    }
    if m.UsedTokens[token] {
        return nil, ErrTokenAlreadyUsed
    }
    m.UsedTokens[token] = true
    return m.StateConfig, nil
}
// ... other methods
```

### Test Scenarios

- Successful registration with token
- Invalid token rejected
- Token single-use enforcement
- SVID-based registration
- Heartbeat success/failure
- Connection errors

---

## 5. HarborClient Interface

Abstracts Harbor registry operations.

### Interface Definition

```go
// Artifact represents an OCI artifact
type Artifact struct {
    Repository string
    Tag        string
    Digest     string
    MediaType  string
    Size       int64
}

// HarborClient provides Harbor registry operations
type HarborClient interface {
    // Authenticate sets up authentication
    Authenticate(username, password string) error

    // AuthenticateWithCert sets up mTLS authentication
    AuthenticateWithCert(cert *tls.Certificate) error

    // PullArtifact pulls an artifact
    PullArtifact(ref string) (io.ReadCloser, error)

    // PushArtifact pushes an artifact
    PushArtifact(ref string, content io.Reader) error

    // ListTags lists tags for a repository
    ListTags(repository string) ([]string, error)

    // GetManifest retrieves artifact manifest
    GetManifest(ref string) (*Manifest, error)

    // DeleteArtifact deletes an artifact
    DeleteArtifact(ref string) error

    // CopyArtifact copies artifact between registries
    CopyArtifact(src, dst string) error
}
```

### Mock Implementation

```go
type MockHarborClient struct {
    Artifacts    map[string][]byte
    Tags         map[string][]string
    Manifests    map[string]*Manifest
    AuthMethod   string
    Err          error
}

func (m *MockHarborClient) PullArtifact(ref string) (io.ReadCloser, error) {
    if m.Err != nil {
        return nil, m.Err
    }
    data, ok := m.Artifacts[ref]
    if !ok {
        return nil, ErrArtifactNotFound
    }
    return io.NopCloser(bytes.NewReader(data)), nil
}
// ... other methods
```

### Test Scenarios

- Pull artifact successfully
- Artifact not found
- Authentication methods (basic, mTLS)
- Copy between registries
- Network errors

---

## 6. CryptoProvider Interface

Abstracts cryptographic operations.

### Interface Definition

```go
// CryptoProvider provides cryptographic operations
type CryptoProvider interface {
    // Encrypt encrypts data with key
    Encrypt(plaintext, key []byte) ([]byte, error)

    // Decrypt decrypts data with key
    Decrypt(ciphertext, key []byte) ([]byte, error)

    // DeriveKey derives key from input using KDF
    DeriveKey(input []byte, salt []byte, keyLen int) ([]byte, error)

    // Sign signs data with private key
    Sign(data []byte, key crypto.PrivateKey) ([]byte, error)

    // Verify verifies signature
    Verify(data, signature []byte, key crypto.PublicKey) error

    // GenerateKeyPair generates new key pair
    GenerateKeyPair() (crypto.PrivateKey, crypto.PublicKey, error)

    // Hash computes hash of data
    Hash(data []byte) []byte

    // RandomBytes generates random bytes
    RandomBytes(n int) ([]byte, error)
}
```

### Mock Implementation

```go
type MockCryptoProvider struct {
    EncryptedData map[string][]byte
    DerivedKeys   map[string][]byte
    Signatures    map[string][]byte
    Err           error
}

func (m *MockCryptoProvider) Encrypt(plaintext, key []byte) ([]byte, error) {
    if m.Err != nil {
        return nil, m.Err
    }
    // Simple mock: prepend "encrypted:" prefix
    result := append([]byte("encrypted:"), plaintext...)
    return result, nil
}

func (m *MockCryptoProvider) Decrypt(ciphertext, key []byte) ([]byte, error) {
    if m.Err != nil {
        return nil, m.Err
    }
    if !bytes.HasPrefix(ciphertext, []byte("encrypted:")) {
        return nil, ErrDecryptionFailed
    }
    return ciphertext[10:], nil
}
// ... other methods
```

### Test Scenarios

- Encrypt/decrypt roundtrip
- Wrong key fails decryption
- Corrupted data detection
- Key derivation determinism
- Signature verification

---

## Additional Utility Mocks

### Clock Interface (for time-based tests)

```go
type Clock interface {
    Now() time.Time
    After(d time.Duration) <-chan time.Time
}

type MockClock struct {
    CurrentTime time.Time
}

func (m *MockClock) Now() time.Time {
    return m.CurrentTime
}

func (m *MockClock) Advance(d time.Duration) {
    m.CurrentTime = m.CurrentTime.Add(d)
}
```

### Logger Interface (for audit tests)

```go
type AuditLogger interface {
    Log(event AuditEvent) error
    Query(filter AuditFilter) ([]AuditEvent, error)
}

type MockAuditLogger struct {
    Events []AuditEvent
}

func (m *MockAuditLogger) Log(event AuditEvent) error {
    m.Events = append(m.Events, event)
    return nil
}
```

---

## Mock Generation Tools

Consider using these tools to generate mocks:

### gomock

```bash
go install github.com/golang/mock/mockgen@latest

# Generate mock
mockgen -source=interfaces.go -destination=mocks/mock_interfaces.go
```

### testify/mock

```go
import "github.com/stretchr/testify/mock"

type MockDeviceIdentity struct {
    mock.Mock
}

func (m *MockDeviceIdentity) GetFingerprint() (string, error) {
    args := m.Called()
    return args.String(0), args.Error(1)
}
```

### mockery

```bash
go install github.com/vektra/mockery/v2@latest

# Generate all mocks
mockery --all --output=mocks
```

---

## File Organization

```
internal/
├── identity/
│   ├── device.go           # DeviceIdentity interface
│   ├── device_linux.go     # Linux implementation
│   └── device_test.go      # Tests with mock
├── tpm/
│   ├── tpm.go              # TPMDevice interface
│   ├── tpm_real.go         # Real TPM implementation
│   ├── tpm_software.go     # swtpm implementation
│   └── tpm_test.go         # Tests with mock
├── spire/
│   ├── client.go           # SPIREClient interface
│   └── client_test.go      # Tests with mock
├── crypto/
│   ├── provider.go         # CryptoProvider interface
│   └── provider_test.go    # Tests with mock
└── mocks/
    ├── device_mock.go
    ├── tpm_mock.go
    ├── spire_mock.go
    ├── groundcontrol_mock.go
    ├── harbor_mock.go
    └── crypto_mock.go
```

---

## Related Documents

- [Test Strategy](./test-strategy.md)
- [Hardware Shopping List](./hardware-shopping-list.md)
