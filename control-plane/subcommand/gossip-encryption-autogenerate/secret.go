package gossipencryptionautogenerate

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

type Secret struct {
	Name string
	Key  string

	value string
}

// Generates a random 32 byte secret
func (s *Secret) Generate() error {
	key := make([]byte, 32)
	n, err := rand.Reader.Read(key)

	if err != nil {
		return fmt.Errorf("error reading random data: %s", err)
	}
	if n != 32 {
		return fmt.Errorf("couldn't read enough entropy")
	}

	s.value = base64.StdEncoding.EncodeToString(key)
	return nil
}

// Value returns the value of the secret
func (s *Secret) Value() string {
	return s.value
}

// PostToKubernetes uses the Kubernetes client to create a secret in the Kubernetes cluster
// at the given Name and Key with the contents of the secret value
func (s *Secret) PostToKubernetes() error {
	if s.value == "" {
		return fmt.Errorf("no secret generated to be stored. Execute Secret.Generate() first")
	}

	// TODO: post secret to Kubernetes

	return nil
}
