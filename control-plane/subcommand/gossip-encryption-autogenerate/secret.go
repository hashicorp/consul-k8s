package gossipencryptionautogenerate

import (
	"fmt"
	"os/exec"
)

type Secret struct {
	Name string
	Key  string

	value string
}

// Generates a random 32 byte secret
func (s *Secret) Generate() error {
	gossipSecret, err := exec.Command("consul", "keygen").Output()

	if err != nil {
		return err
	} else {
		s.value = string(gossipSecret)
		return nil
	}
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
