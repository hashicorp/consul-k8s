// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

// bootstrapServers bootstraps ACLs and ensures each server has an ACL token.
// If a bootstrap is found in the secrets backend, then skip ACL bootstrapping.
// Otherwise, bootstrap ACLs and write the bootstrap token to the secrets backend.
func (c *Command) bootstrapServers(serverAddresses []net.IPAddr, backend SecretsBackend) (string, error) {
	// Pick the first server address to connect to for bootstrapping and set up connection.
	firstServerAddr := fmt.Sprintf("%s:%d", serverAddresses[0].IP.String(), c.consulFlags.HTTPPort)

	bootstrapToken, err := backend.BootstrapToken()
	if err != nil {
		return "", fmt.Errorf("Unexpected error fetching bootstrap token secret: %w", err)
	}

	if bootstrapToken != "" {
		c.log.Info("Found bootstrap token in secrets backend", "secret", backend.BootstrapTokenSecretName())
	} else {
		c.log.Info("No bootstrap token found in secrets backend, continuing to ACL bootstrapping", "secret", backend.BootstrapTokenSecretName())
		bootstrapToken, err = c.bootstrapACLs(firstServerAddr, backend)
		if err != nil {
			return "", err
		}
	}

	// We should only create and set server tokens when servers are running within this cluster.
	if c.flagSetServerTokens {
		c.log.Info("Setting Consul server tokens")
		// Create new tokens for each server and apply them.
		if err := c.setServerTokens(serverAddresses, bootstrapToken); err != nil {
			return "", err
		}
	}
	return bootstrapToken, nil
}

// bootstrapACLs makes the ACL bootstrap API call and writes the bootstrap token
// to a kube secret.
func (c *Command) bootstrapACLs(firstServerAddr string, backend SecretsBackend) (string, error) {
	config := c.consulFlags.ConsulClientConfig().APIClientConfig
	config.Address = firstServerAddr
	// Exempting this particular use of the http client from using global.consulAPITimeout
	// which defaults to 5 seconds.  In acceptance tests, we saw that the call
	// to /v1/acl/bootstrap taking 5-7 seconds and when it does, the request times
	// out without returning the bootstrap token, but the bootstrapping does complete.
	// This would leave cases where server-acl-init job would get a 403 that it had
	// already bootstrapped and would not be able to complete.
	// Since this is an area where we have to wait and can't retry, we are setting it
	// to a large number like 5 minutes since previously this had no timeout.
	config.HttpClient = &http.Client{Timeout: 5 * time.Minute}
	consulClient, err := consul.NewClient(config, c.consulFlags.APITimeout)

	if err != nil {
		return "", fmt.Errorf("creating Consul client for address %s: %s", firstServerAddr, err)
	}

	// Call bootstrap ACLs API.
	var bootstrapToken string
	var unrecoverableErr error
	err = c.untilSucceeds("bootstrapping ACLs - PUT /v1/acl/bootstrap",
		func() error {
			bootstrapResp, _, err := consulClient.ACL().Bootstrap()
			if err == nil {
				bootstrapToken = bootstrapResp.SecretID
				return nil
			}

			// Check if already bootstrapped.
			if strings.Contains(err.Error(), "Unexpected response code: 403") {
				unrecoverableErr = fmt.Errorf(
					"ACLs already bootstrapped but unable to find the bootstrap token in the secrets backend."+
						" We can't proceed without a bootstrap token."+
						" Store a token with `acl:write` permission in the secret %q.",
					backend.BootstrapTokenSecretName(),
				)
				return nil
			}

			if isNoLeaderErr(err) {
				// Return a more descriptive error in the case of no leader
				// being elected.
				return fmt.Errorf("no leader elected: %s", err)
			}
			return err
		})
	if unrecoverableErr != nil {
		return "", unrecoverableErr
	}
	if err != nil {
		return "", err
	}

	// Write bootstrap token to the secrets backend.
	err = c.untilSucceeds(fmt.Sprintf("writing bootstrap Secret %q", backend.BootstrapTokenSecretName()),
		func() error {
			return backend.WriteBootstrapToken(bootstrapToken)
		},
	)
	return bootstrapToken, err
}

// setServerTokens creates policies and associated ACL token for each server
// and then provides the token to the server.
func (c *Command) setServerTokens(serverAddresses []net.IPAddr, bootstrapToken string) error {
	// server specifically.
	clientConfig := c.consulFlags.ConsulClientConfig().APIClientConfig
	clientConfig.Address = fmt.Sprintf("%s:%d", serverAddresses[0].IP.String(), c.consulFlags.HTTPPort)
	clientConfig.Token = bootstrapToken
	client, err := consul.NewDynamicClientWithTimeout(clientConfig,
		c.consulFlags.APITimeout)
	if err != nil {
		return err
	}
	agentPolicy, err := c.setServerPolicy(client)
	if err != nil {
		return err
	}

	existingTokens, _, err := client.ConsulClient.ACL().TokenList(nil)
	if err != nil {
		return err
	}

	// Create agent token for each server agent.
	for _, host := range serverAddresses {
		var tokenSecretID string

		// We create a new client for each server because we need to call each
		// server specifically.
		clientConfig := c.consulFlags.ConsulClientConfig().APIClientConfig
		clientConfig.Address = fmt.Sprintf("%s:%d", host.IP.String(), c.consulFlags.HTTPPort)
		clientConfig.Token = bootstrapToken
		serverClient, err := consul.NewClient(clientConfig,
			c.consulFlags.APITimeout)
		if err != nil {
			return err
		}

		tokenDescription := fmt.Sprintf("Server Token for %s", host.IP.String())

		// Check if the token was already created. We're matching on the description
		// since that's the only part that's unique.
		for _, t := range existingTokens {
			if len(t.Policies) == 1 && t.Policies[0].Name == agentPolicy.Name {
				if t.Description == tokenDescription {
					tokenSecretID = t.SecretID
					break
				}
			}
		}

		// Create token for the server if it doesn't already exist.
		if tokenSecretID == "" {
			err = c.untilSucceeds(fmt.Sprintf("creating server token for %s - PUT /v1/acl/token", host),
				func() error {
					tokenReq := api.ACLToken{
						Description: tokenDescription,
						Policies:    []*api.ACLTokenPolicyLink{{Name: agentPolicy.Name}},
					}
					token, _, err := serverClient.ACL().TokenCreate(&tokenReq, nil)
					if err != nil {
						return err
					}
					tokenSecretID = token.SecretID
					return nil
				})
			if err != nil {
				return err
			}
		}

		// Pass out agent tokens to servers. It's okay to make this API call
		// even if the server already has a token since the call is idempotent.
		err = c.untilSucceeds(fmt.Sprintf("updating server token for %s - PUT /v1/agent/token/agent", host),
			func() error {
				_, err := serverClient.Agent().UpdateAgentACLToken(tokenSecretID, nil)
				return err
			})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Command) setServerPolicy(client *consul.DynamicClient) (api.ACLPolicy, error) {
	agentRules, err := c.agentRules()
	if err != nil {
		c.log.Error("Error templating server agent rules", "err", err)
		return api.ACLPolicy{}, err
	}

	// Create agent policy.
	agentPolicy := api.ACLPolicy{
		Name:        "agent-token",
		Description: "Agent Token Policy",
		Rules:       agentRules,
	}
	err = c.untilSucceeds("creating agent policy - PUT /v1/acl/policy",
		func() error {
			return c.createOrUpdateACLPolicy(agentPolicy, client)
		})
	if err != nil {
		return api.ACLPolicy{}, err
	}

	return agentPolicy, nil
}

// isNoLeaderErr returns true if err is due to trying to call the
// bootstrap ACLs API when there is no leader elected.
func isNoLeaderErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Unexpected response code: 500") &&
		strings.Contains(err.Error(), "The ACL system is currently in legacy mode.")
}
