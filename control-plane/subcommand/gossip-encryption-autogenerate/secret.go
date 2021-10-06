package gossipencryptionautogenerate

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
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

// Write uses the Kubernetes client to create a secret in the Kubernetes cluster
// at the given Name and Key with the contents of the secret value
func (s *Secret) Write(k8sSecretClient corev1.SecretInterface) error {
	if s.value == "" {
		return fmt.Errorf("no secret generated to be stored. Execute Secret.Generate() first")
	}

	_, err := k8sSecretClient.Create(
		context.TODO(),
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: s.Name,
			},
			Data: map[string][]byte{
				s.Key: []byte(s.value),
			},
		},
		metav1.CreateOptions{})

	return err
}
