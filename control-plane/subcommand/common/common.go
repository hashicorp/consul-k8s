// Package common holds code needed by multiple commands.
package common

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
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

	// The number of times to attempt ACL Login.
	numLoginRetries = 100

	raftReplicationTimeout   = 2 * time.Second
	tokenReadPollingInterval = 100 * time.Millisecond
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
func ConsulLogin(client *api.Client, cfg *api.Config, authMethodName, datacenter, namespace, bearerTokenFile, serviceAccountName, tokenSinkFile string, meta map[string]string, log hclog.Logger) (string, error) {
	// Read the bearerTokenFile.
	data, err := ioutil.ReadFile(bearerTokenFile)
	if err != nil {
		return "", fmt.Errorf("unable to read bearerTokenFile: %v, err: %v", bearerTokenFile, err)
	}
	bearerToken := strings.TrimSpace(string(data))
	if bearerToken == "" {
		return "", fmt.Errorf("no bearer token found in %s", bearerTokenFile)
	}
	err = backoff.Retry(func() error {
		// Do the login.
		req := &api.ACLLoginParams{
			AuthMethod:  authMethodName,
			BearerToken: bearerToken,
			Meta:        meta,
		}
		// The datacenter flag will either have the value of the primary datacenter or "". In case of the latter,
		// the token will be created in the datacenter of the installation. In case a global token is required,
		// the token will be created in the primary datacenter.
		tok, _, err := client.ACL().Login(req, &api.WriteOptions{Namespace: namespace, Datacenter: datacenter})
		if err != nil {
			log.Error("unable to login", "error", err)
			return fmt.Errorf("error logging in: %s", err)
		}
		if tokenSinkFile != "" {
			// Write out the resultant token file.
			// Must be 0644 because this is written by the consul-k8s user but needs
			// to be readable by the consul user
			if err := WriteFileWithPerms(tokenSinkFile, tok.SecretID, 0644); err != nil {
				return fmt.Errorf("error writing token to file sink: %v", err)
			}
		}
		return err
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), numLoginRetries))
	if err != nil {
		if serviceAccountName == "default" {
			log.Warn("The service account name for this Pod is \"default\"." +
				" In default installations this is not a supported service account name." +
				" The service account name must match the name of the Kubernetes Service" +
				" or the consul.hashicorp.com/connect-service annotation.")
		}
		log.Error("Hit maximum retries for consul login", "error", err)
		return "", err
	}
	// Now update the client so that it will read the ACL token we just fetched.
	cfg.TokenFile = tokenSinkFile
	client, err = consul.NewClient(cfg)
	if err != nil {
		log.Error("Unable to update client connection", "error", err)
		return "", err
	}
	log.Info("Consul login complete")

	// A workaround to check that the ACL token is replicated to other Consul servers.
	//
	// A consul client may reach out to a follower instead of a leader to resolve the token during the
	// call to get services below. This is because clients talk to servers in the stale consistency mode
	// to decrease the load on the servers (see https://www.consul.io/docs/architecture/consensus#stale).
	// In that case, it's possible that the token isn't replicated
	// to that server instance yet. The client will then get an "ACL not found" error
	// and subsequently cache this not found response. Then our call below
	// to get services from the agent will keep hitting the same "ACL not found" error
	// until the cache entry expires (determined by the `acl_token_ttl` which defaults to 30 seconds).
	// This is not great because it will delay app start up time by 30 seconds in most cases
	// (if you are running 3 servers, then the probability of ending up on a follower is close to 2/3).
	//
	// To help with that, we try to first read the token in the stale consistency mode until we
	// get a successful response. This should not take more than 100ms because raft replication
	// should in most cases take less than that (see https://www.consul.io/docs/install/performance#read-write-tuning)
	// but we set the timeout to 2s to be sure.
	//
	// Note though that this workaround does not eliminate this problem completely. It's still possible
	// for this call and the next call to reach different servers and those servers to have different
	// states from each other.
	// For example, this call can reach a leader and succeed, while the call below can go to a follower
	// that is still behind the leader and get an "ACL not found" error.
	// However, this is a pretty unlikely case because
	// clients have sticky connections to a server, and those connections get rebalanced only every 2-3min.
	// And so, this workaround should work in a vast majority of cases.
	log.Info("Checking that the ACL token exists when reading it in the stale consistency mode")
	// Use raft timeout and polling interval to determine the number of retries.
	numTokenReadRetries := uint64(raftReplicationTimeout.Milliseconds() / tokenReadPollingInterval.Milliseconds())
	var aclLoginToken *api.ACLToken
	err = backoff.Retry(func() error {
		aclLoginToken, _, err = client.ACL().TokenReadSelf(&api.QueryOptions{AllowStale: true})
		if err != nil {
			log.Error("Unable to read ACL token; retrying", "err", err)
		}
		return err
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(tokenReadPollingInterval), numTokenReadRetries))
	if err != nil {
		log.Error("Unable to read ACL token from a Consul server; "+
			"please check that your server cluster is healthy", "err", err)
		return "", err
	}
	log.Info("Successfully read ACL token from the server")
	return aclLoginToken.SecretID, nil
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
