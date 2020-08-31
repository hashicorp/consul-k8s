package framework

import (
	"flag"
	"fmt"
	"io/ioutil"
	"testing"
)

type suite struct {
	m     *testing.M
	env   *kubernetesEnvironment
	cfg   *TestConfig
	flags *TestFlags
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
		m:     m,
		env:   newKubernetesEnvironmentFromConfig(testConfig),
		cfg:   testConfig,
		flags: flags,
	}
}

func (s *suite) Run() int {
	err := s.flags.validate()
	if err != nil {
		fmt.Printf("Flag validation failed: %s\n", err)
		return 1
	}

	// Create test debug directory if it doesn't exist
	if s.cfg.DebugDirectory == "" {
		var err error
		s.cfg.DebugDirectory, err = ioutil.TempDir("", "consul-test")
		if err != nil {
			fmt.Printf("Failed to create debug directory: %s\n", err)
			return 1
		}
	}

	return s.m.Run()
}

func (s *suite) Environment() TestEnvironment {
	return s.env
}

func (s *suite) Config() *TestConfig {
	return s.cfg
}
