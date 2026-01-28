package middleware

import (
	"crypto/tls"
	"log"
	"os"
	"sync"
	"time"
)

// CertWatcher watches certificate files and reloads them when changed.
type CertWatcher struct {
	certFile    string
	keyFile     string
	cert        *tls.Certificate
	mu          sync.RWMutex
	lastModTime time.Time
	stopCh      chan struct{}
}

// NewCertWatcher creates a new certificate watcher.
func NewCertWatcher(certFile, keyFile string) (*CertWatcher, error) {
	cw := &CertWatcher{
		certFile: certFile,
		keyFile:  keyFile,
		stopCh:   make(chan struct{}),
	}

	// Load initial certificate
	if err := cw.loadCertificate(); err != nil {
		return nil, err
	}

	return cw, nil
}

// GetCertificate returns the current certificate for use with tls.Config.GetCertificate.
func (cw *CertWatcher) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.cert, nil
}

// GetClientCertificate returns the current certificate for use with tls.Config.GetClientCertificate.
func (cw *CertWatcher) GetClientCertificate(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.cert, nil
}

// Start begins watching the certificate files for changes.
func (cw *CertWatcher) Start(checkInterval time.Duration) {
	ticker := time.NewTicker(checkInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if cw.hasChanged() {
					if err := cw.loadCertificate(); err != nil {
						log.Printf("Failed to reload certificate: %v", err)
					} else {
						log.Println("Certificate reloaded successfully")
					}
				}
			case <-cw.stopCh:
				return
			}
		}
	}()
}

// Stop stops the certificate watcher.
func (cw *CertWatcher) Stop() {
	close(cw.stopCh)
}

// loadCertificate loads the certificate from files.
func (cw *CertWatcher) loadCertificate() error {
	cert, err := tls.LoadX509KeyPair(cw.certFile, cw.keyFile)
	if err != nil {
		return err
	}

	cw.mu.Lock()
	cw.cert = &cert
	cw.updateModTime()
	cw.mu.Unlock()

	return nil
}

// hasChanged checks if the certificate files have been modified.
func (cw *CertWatcher) hasChanged() bool {
	certInfo, err := os.Stat(cw.certFile)
	if err != nil {
		return false
	}

	keyInfo, err := os.Stat(cw.keyFile)
	if err != nil {
		return false
	}

	// Check if either file has been modified after our last load
	cw.mu.RLock()
	lastMod := cw.lastModTime
	cw.mu.RUnlock()

	return certInfo.ModTime().After(lastMod) || keyInfo.ModTime().After(lastMod)
}

// updateModTime updates the last modification time.
func (cw *CertWatcher) updateModTime() {
	certInfo, err := os.Stat(cw.certFile)
	if err != nil {
		return
	}

	keyInfo, err := os.Stat(cw.keyFile)
	if err != nil {
		return
	}

	// Use the later of the two modification times
	if certInfo.ModTime().After(keyInfo.ModTime()) {
		cw.lastModTime = certInfo.ModTime()
	} else {
		cw.lastModTime = keyInfo.ModTime()
	}
}
