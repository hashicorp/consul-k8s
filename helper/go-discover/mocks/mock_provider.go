package mocks

import (
	"log"

	"github.com/stretchr/testify/mock"
)

type MockProvider struct {
	mock.Mock
}

func (m *MockProvider) Addrs(args map[string]string, l *log.Logger) ([]string, error) {
	retVal := m.Called(args, l)
	addresses := retVal.Get(0)
	if addresses != nil {
		return addresses.([]string), nil
	}
	return nil, retVal.Error(1)
}

func (m *MockProvider) Help() string {
	return "mock-provider help"
}
