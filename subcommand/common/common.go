// Package common holds code needed by multiple commands.
package common

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/hashicorp/go-hclog"
)

const (
	// ACLReplicationTokenName is the name used for the ACL replication policy and
	// Kubernetes secret. It is consumed in both the server-acl-init and
	// create-federation-secret commands and so lives in this common package.
	ACLReplicationTokenName = "acl-replication"

	// ACLTokenSecretKey is the key that we store the ACL tokens in when we
	// create Kubernetes secrets.
	ACLTokenSecretKey = "token"
)

// Logger returns an hclog instance or an error if level is invalid.
func Logger(level string) (hclog.Logger, error) {
	parsedLevel := hclog.LevelFromString(level)
	if parsedLevel == hclog.NoLevel {
		return nil, fmt.Errorf("unknown log level: %s", level)
	}
	return hclog.New(&hclog.LoggerOptions{
		Level:  parsedLevel,
		Output: os.Stderr,
	}), nil
}

// ValidateUnprivilegedPort converts flags representing ports into integer and validates
// that it's in the unprivileged port range.
func ValidateUnprivilegedPort(flagName, flagValue string) error {
	port, err := strconv.Atoi(flagValue)
	if err != nil {
		return errors.New(fmt.Sprintf("%s value of %s is not a valid integer", flagName, flagValue))
	}
	// This checks if the port is in the unprivileged port range.
	if port < 1024 || port > 65535 {
		return errors.New(fmt.Sprintf("%s value of %d is not in the unprivileged port range 1024-65535", flagName, port))
	}
	return nil
}
