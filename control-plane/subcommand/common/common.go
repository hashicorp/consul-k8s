// Package common holds code needed by multiple commands.
package common

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	godiscover "github.com/hashicorp/consul-k8s/control-plane/helper/go-discover"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-discover"
	"github.com/hashicorp/go-hclog"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	// ACLReplicationTokenName is the name used for the ACL replication policy and
	// Kubernetes secret. It is consumed in both the server-acl-init and
	// create-federation-secret commands and so lives in this common package.
	ACLReplicationTokenName = "acl-replication"

	// ACLTokenSecretKey is the key that we store the ACL tokens in when we
	// create Kubernetes secrets.
	ACLTokenSecretKey = "token"

	// CLILabelKey and CLILabelValue are added to each secret on creation so the CLI knows
	// which secrets to delete on an uninstall.
	CLILabelKey   = "managed-by"
	CLILabelValue = "consul-k8s"
)

// Logger returns an hclog instance with log level set and JSON logging enabled/disabled, or an error if level is invalid.
func Logger(level string, jsonLogging bool) (hclog.Logger, error) {
	parsedLevel := hclog.LevelFromString(level)
	if parsedLevel == hclog.NoLevel {
		return nil, fmt.Errorf("unknown log level: %s", level)
	}
	return hclog.New(&hclog.LoggerOptions{
		JSONFormat: jsonLogging,
		Level:      parsedLevel,
		Output:     os.Stderr,
	}), nil
}

// ZapLogger returns a logr.Logger instance with log level set and JSON logging enabled/disabled, or an error if the level is invalid.
func ZapLogger(level string, jsonLogging bool) (logr.Logger, error) {
	var zapLevel zapcore.Level
	// It is possible that a user passes in "trace" from global.logLevel, until we standardize on one logging framework
	// we will assume they meant debug here and not fail.
	if level == "trace" || level == "TRACE" {
		level = "debug"
	}
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("unknown log level %q: %s", level, err.Error())
	}
	if jsonLogging {
		return zap.New(zap.UseDevMode(false), zap.Level(zapLevel), zap.JSONEncoder()), nil
	}
	return zap.New(zap.UseDevMode(false), zap.Level(zapLevel), zap.ConsoleEncoder()), nil
}

// ValidateUnprivilegedPort converts flags representing ports into integer and validates
// that it's in the unprivileged port range.
func ValidateUnprivilegedPort(flagName, flagValue string) error {
	port, err := strconv.Atoi(flagValue)
	if err != nil {
		return fmt.Errorf("%s value of %s is not a valid integer", flagName, flagValue)
	}
	// This checks if the port is in the unprivileged port range.
	if port < 1024 || port > 65535 {
		return fmt.Errorf("%s value of %d is not in the unprivileged port range 1024-65535", flagName, port)
	}
	return nil
}

// ConsulLogin issues an ACL().Login to Consul and writes out the token to tokenSinkFile.
// The logic of this is taken from the `consul login` command.
func ConsulLogin(client *api.Client, bearerTokenFile, authMethodName, tokenSinkFile, namespace string, meta map[string]string) error {
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
	tok, _, err := client.ACL().Login(req, &api.WriteOptions{Namespace: namespace})
	if err != nil {
		return fmt.Errorf("error logging in: %s", err)
	}

	if err := WriteFileWithPerms(tokenSinkFile, tok.SecretID, 0444); err != nil {
		return fmt.Errorf("error writing token to file sink: %v", err)
	}
	return nil
}

// WriteFileWithPerms will write payload as the contents of the outputFile and set permissions after writing the contents. This function is necessary since using ioutil.WriteFile() alone will create the new file with the requested permissions prior to actually writing the file, so you can't set read-only permissions.
func WriteFileWithPerms(outputFile, payload string, mode os.FileMode) error {
	// os.WriteFile truncates existing files and overwrites them, but only if they are writable.
	// If the file exists it will already likely be read-only. Remove it first.
	if _, err := os.Stat(outputFile); err == nil {
		if err = os.Remove(outputFile); err != nil {
			return fmt.Errorf("unable to delete existing file: %s", err)
		}
	}
	if err := ioutil.WriteFile(outputFile, []byte(payload), os.ModePerm); err != nil {
		return fmt.Errorf("unable to write file: %s", err)
	}
	return os.Chmod(outputFile, mode)
}

// GetResolvedServerAddresses resolves the Consul server address if it has been provided a provider else it returns the server addresses that were input to it.
// It attempts to use go-discover iff there is a single server address, the value of which begins with "provider=", else it returns the server addresses as is.
func GetResolvedServerAddresses(serverAddresses []string, providers map[string]discover.Provider, logger hclog.Logger) ([]string, error) {
	if len(serverAddresses) != 1 || !strings.Contains(serverAddresses[0], "provider=") {
		return serverAddresses, nil
	}
	return godiscover.ConsulServerAddresses(serverAddresses[0], providers, logger)
}
