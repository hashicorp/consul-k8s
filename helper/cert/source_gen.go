package cert

import (
	"context"
	"crypto"
	"crypto/x509"
	"sync"
	"time"
)

// GenSource generates a self-signed CA and certificate pair.
//
// This generator is stateful. On the first run (last == nil to Certificate),
// a CA will be generated. On subsequent calls, the same CA will be used to
// create a new certificate when the expiry is near. To create a new CA, a
// new GenSource must be allocated.
type GenSource struct {
	Name  string   // Name is used as part of the common name
	Hosts []string // Hosts is the list of hosts to make the leaf valid for

	// Expiry is the duration that a certificate is valid for. This
	// defaults to 24 hours.
	Expiry time.Duration

	// ExpiryWithin is the duration value used for determining whether to
	// regenerate a new leaf certificate. If the old leaf certificate is
	// expiring within this value, then a new leaf will be generated. Default
	// is about 10% of Expiry.
	ExpiryWithin time.Duration

	mu             sync.Mutex
	caCert         string
	caCertTemplate *x509.Certificate
	caSigner       crypto.Signer
}

// Certificate implements Source
func (s *GenSource) Certificate(ctx context.Context, last *Bundle) (Bundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result Bundle

	// If we have no CA, generate it for the first time.
	if len(s.caCert) == 0 {
		if err := s.generateCA(); err != nil {
			return result, err
		}
	}
	// Set the CA cert
	result.CACert = []byte(s.caCert)

	// If we have a prior cert, we wait for getting near to the expiry
	// (within 30 minutes arbitrarily chosen).
	if last != nil {
		// We have a prior certificate, let's parse it to get the expiry
		cert, err := ParseCert(last.Cert)
		if err != nil {
			return result, err
		}

		waitTime := time.Until(cert.NotAfter) - s.expiryWithin()
		if waitTime < 0 {
			waitTime = 1 * time.Millisecond
		}

		timer := time.NewTimer(waitTime)
		defer timer.Stop()

		select {
		case <-timer.C:
			// Fall through, generate cert

		case <-ctx.Done():
			return result, ctx.Err()
		}
	}

	// Generate cert, set it on the result, and return
	cert, key, err := GenerateCert(s.Name+" Service", s.expiry(), s.caCertTemplate, s.caSigner, s.Hosts)
	if err == nil {
		result.Cert = []byte(cert)
		result.Key = []byte(key)
	}

	return result, err
}

func (s *GenSource) expiry() time.Duration {
	if s.Expiry > 0 {
		return s.Expiry
	}

	return 24 * time.Hour
}

func (s *GenSource) expiryWithin() time.Duration {
	if s.ExpiryWithin > 0 {
		return s.ExpiryWithin
	}

	// Roughly 10% accounting for float errors
	return time.Duration(float64(s.expiry()) * 0.10)
}

func (s *GenSource) generateCA() error {
	// generate the CA
	signer, _, caCertPem, caCertTemplate, err := GenerateCA(s.Name + " CA")
	if err != nil {
		return err
	}
	s.caSigner = signer
	s.caCert = caCertPem
	s.caCertTemplate = caCertTemplate

	return nil
}
