package mocks

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/helper/cert"
	"github.com/stretchr/testify/mock"
)

type MockCertSource struct {
	mock.Mock
}

func (m *MockCertSource) Certificate(_ context.Context, _ *cert.Bundle) (cert.Bundle, error) {
	result := cert.Bundle{
		Cert:   []byte(fmt.Sprintf("certificate-string-%d", time.Now().Unix())),
		Key:    []byte(fmt.Sprintf("private-key-string-%d", time.Now().Unix())),
		CACert: []byte(fmt.Sprintf("ca-certificate-string-%d", time.Now().Unix())),
	}
	return result, nil
}
