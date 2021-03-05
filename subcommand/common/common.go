// Package common holds code needed by multiple commands.
package common

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/hashicorp/consul/api"
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

// ValidatePort converts flags representing ports into integer and validates
// that it's in the port range.
func ValidatePort(flagName, flagValue string) error {
	port, err := strconv.Atoi(flagValue)
	if err != nil {
		return errors.New(fmt.Sprintf("%s value of %s is not a valid integer.", flagName, flagValue))
	}
	// This checks if the port is in the valid port range.
	if port < 1024 || port > 65535 {
		return errors.New(fmt.Sprintf("%s value of %d is not in the port range 1024-65535.", flagName, port))
	}
	return nil
}

// ConsulLogin issues an ACL().Login to Consul and writes out the token to tokenSinkFile.
// The logic of this is taken from the `consul login` command.
func ConsulLogin(client *api.Client, bearerTokenFile, authMethodName, tokenSinkFile string, meta map[string]string) error {
	if meta == nil {
		return fmt.Errorf("invalid meta")
	}
	data, err := ioutil.ReadFile(bearerTokenFile)
	if err != nil {
		return fmt.Errorf("unable to read bearerTokenFile: %v, err: %v", bearerTokenFile, err)
	}
	bearerToken := strings.TrimSpace(string(data))
	if bearerToken == "" {
		return fmt.Errorf("no bearer token found in %s", bearerTokenFile)
	}
	// Do the login.
	req := &api.ACLLoginParams{
		AuthMethod:  authMethodName,
		BearerToken: bearerToken,
		Meta:        meta,
	}
	tok, _, err := client.ACL().Login(req, nil)
	if err != nil {
		return fmt.Errorf("error logging in: %s", err)
	}

	// Write the token out to file with permissions so consul-k8s user can read.
	payload := []byte(tok.SecretID)
	if err := ioutil.WriteFile(tokenSinkFile, payload, 0444); err != nil {
		return fmt.Errorf("error writing token to file sink: %v", err)
	}
	return nil
}
