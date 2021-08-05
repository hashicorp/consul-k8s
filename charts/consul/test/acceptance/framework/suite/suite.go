package suite

import (
	"flag"
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework/config"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/environment"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/flags"
)

type suite struct {
	m     *testing.M
	env   *environment.KubernetesEnvironment
	cfg   *config.TestConfig
	flags *flags.TestFlags
}

type Suite interface {
	Run() int
	Environment() environment.TestEnvironment
	Config() *config.TestConfig
}

func NewSuite(m *testing.M) Suite {
	flags := flags.NewTestFlags()

	flag.Parse()

	testConfig := flags.TestConfigFromFlags()

	return &suite{
		m:     m,
		env:   environment.NewKubernetesEnvironmentFromConfig(testConfig),
		cfg:   testConfig,
		flags: flags,
	}
}

func (s *suite) Run() int {
	err := s.flags.Validate()
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

func (s *suite) Environment() environment.TestEnvironment {
	return s.env
}

func (s *suite) Config() *config.TestConfig {
	return s.cfg
}
