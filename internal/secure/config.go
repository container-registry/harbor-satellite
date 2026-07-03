package secure

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/container-registry/harbor-satellite/internal/crypto"
	"github.com/container-registry/harbor-satellite/internal/identity"
)

var (
	ErrConfigNotFound   = errors.New("config file not found")
	ErrConfigCorrupted  = errors.New("config file corrupted")
	ErrEncryptionFailed = errors.New("encryption failed")
	ErrDecryptionFailed = errors.New("decryption failed")
	ErrKeyDeriveFailed  = errors.New("key derivation failed")
	ErrInvalidConfig    = errors.New("invalid config")
)

const (
	saltSize = 16
	keySize  = 32
)

// ConfigEncryptor handles encryption and decryption of configuration files.
type ConfigEncryptor struct {
	crypto crypto.Provider
	device identity.DeviceIdentity
}

// NewConfigEncryptor creates a new ConfigEncryptor.
func NewConfigEncryptor(cryptoProvider crypto.Provider, device identity.DeviceIdentity) *ConfigEncryptor {
	return &ConfigEncryptor{
		crypto: cryptoProvider,
		device: device,
	}
}

// EncryptedConfig represents an encrypted configuration with metadata.
type EncryptedConfig struct {
	Version   int    `json:"version"`
	Salt      []byte `json:"salt"`
	Encrypted []byte `json:"data"`
}

// EncryptConfig encrypts a configuration struct and returns encrypted bytes.
func (e *ConfigEncryptor) EncryptConfig(config any) ([]byte, error) {
	plaintext, err := json.Marshal(config)
	if err != nil {
		return nil, ErrInvalidConfig
	}

	return e.EncryptBytes(plaintext)
}

// EncryptBytes encrypts raw bytes and returns the encrypted result.
func (e *ConfigEncryptor) EncryptBytes(plaintext []byte) ([]byte, error) {
	key, salt, err := e.deriveKey()
	if err != nil {
		return nil, err
	}

	encrypted, err := e.crypto.Encrypt(plaintext, key)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	encConfig := EncryptedConfig{
		Version:   1,
		Salt:      salt,
		Encrypted: encrypted,
	}

	return json.Marshal(encConfig)
}

// DecryptConfig decrypts encrypted bytes and unmarshals into the provided struct.
func (e *ConfigEncryptor) DecryptConfig(data []byte, config any) error {
	plaintext, err := e.DecryptBytes(data)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(plaintext, config); err != nil {
		return ErrConfigCorrupted
	}

	return nil
}

// DecryptBytes decrypts encrypted bytes and returns the plaintext.
func (e *ConfigEncryptor) DecryptBytes(data []byte) ([]byte, error) {
	var encConfig EncryptedConfig
	if err := json.Unmarshal(data, &encConfig); err != nil {
		return nil, ErrConfigCorrupted
	}

	key, err := e.deriveKeyWithSalt(encConfig.Salt)
	if err != nil {
		return nil, err
	}

	plaintext, err := e.crypto.Decrypt(encConfig.Encrypted, key)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// EncryptToFile encrypts config and writes to file.
func (e *ConfigEncryptor) EncryptToFile(path string, config any) error {
	data, err := e.EncryptConfig(config)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// DecryptFromFile reads and decrypts config from file.
func (e *ConfigEncryptor) DecryptFromFile(path string, config any) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return ErrConfigNotFound
		}
		return err
	}

	return e.DecryptConfig(data, config)
}

// ReEncrypt decrypts and re-encrypts with a new salt.
func (e *ConfigEncryptor) ReEncrypt(data []byte) ([]byte, error) {
	plaintext, err := e.DecryptBytes(data)
	if err != nil {
		return nil, err
	}

	return e.EncryptBytes(plaintext)
}

// IsEncrypted checks if data appears to be an encrypted config.
func IsEncrypted(data []byte) bool {
	var encConfig EncryptedConfig
	if err := json.Unmarshal(data, &encConfig); err != nil {
		return false
	}
	return encConfig.Version > 0 && len(encConfig.Encrypted) > 0
}

func (e *ConfigEncryptor) deriveKey() ([]byte, []byte, error) {
	salt, err := e.crypto.RandomBytes(saltSize)
	if err != nil {
		return nil, nil, ErrKeyDeriveFailed
	}

	key, err := e.deriveKeyWithSalt(salt)
	if err != nil {
		return nil, nil, err
	}

	return key, salt, nil
}

func (e *ConfigEncryptor) deriveKeyWithSalt(salt []byte) ([]byte, error) {
	fingerprint, err := e.device.GetFingerprint()
	if err != nil {
		return nil, ErrKeyDeriveFailed
	}

	key, err := e.crypto.DeriveKey([]byte(fingerprint), salt, keySize)
	if err != nil {
		return nil, ErrKeyDeriveFailed
	}

	return key, nil
}
