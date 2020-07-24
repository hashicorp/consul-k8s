package framework

import (
	"flag"
	"testing"
)

type suite struct {
	m   *testing.M
	env *kubernetesEnvironment
	cfg *TestConfig
}

type Suite interface {
	Run() int
	Environment() TestEnvironment
	Config() *TestConfig
}

func NewSuite(m *testing.M) Suite {
	flags := NewTestFlags()

	flag.Parse()

	testConfig := flags.testConfigFromFlags()

	return &suite{
		m:   m,
		env: newKubernetesEnvironmentFromConfig(testConfig),
		cfg: testConfig,
	}
}

func (s *suite) Run() int {
	return s.m.Run()
}

func (s *suite) Environment() TestEnvironment {
	return s.env
}

func (s *suite) Config() *TestConfig {
	return s.cfg
}
