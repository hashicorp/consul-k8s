// Package common holds code needed by multiple commands.
package common

import (
	"fmt"
	"os"

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
