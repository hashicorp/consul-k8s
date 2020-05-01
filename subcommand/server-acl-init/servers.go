package serveraclinit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul/api"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// bootstrapServers bootstraps ACLs and ensures each server has an ACL token.
func (c *Command) bootstrapServers(serverAddresses []string, bootTokenSecretName, scheme string) (string, error) {
	// Pick the first server address to connect to for bootstrapping and set up connection.
	firstServerAddr := fmt.Sprintf("%s:%d", serverAddresses[0], c.flagServerPort)
	consulClient, err := api.NewClient(&api.Config{
		Address: firstServerAddr,
		Scheme:  scheme,
		TLSConfig: api.TLSConfig{
			Address: c.flagConsulTLSServerName,
			CAFile:  c.flagConsulCACert,
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating Consul client for address %s: %s", firstServerAddr, err)
	}

	// Call bootstrap ACLs API.
	var bootstrapToken []byte
	var unrecoverableErr error
	err = c.untilSucceeds("bootstrapping ACLs - PUT /v1/acl/bootstrap",
		func() error {
			bootstrapResp, _, err := consulClient.ACL().Bootstrap()
			if err == nil {
				bootstrapToken = []byte(bootstrapResp.SecretID)
				return nil
			}

			// Check if already bootstrapped.
			if strings.Contains(err.Error(), "Unexpected response code: 403") {
				unrecoverableErr = errors.New("ACLs already bootstrapped but the ACL token was not written to a Kubernetes secret." +
					" We can't proceed because the bootstrap token is lost." +
					" You must reset ACLs.")
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

	// Write bootstrap token to a Kubernetes secret.
	err = c.untilSucceeds(fmt.Sprintf("writing bootstrap Secret %q", bootTokenSecretName),
		func() error {
			secret := &apiv1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: bootTokenSecretName,
				},
				Data: map[string][]byte{
					common.ACLTokenSecretKey: bootstrapToken,
				},
			}
			_, err := c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Create(secret)
			return err
		})
	if err != nil {
		return "", err
	}

	// Override our original client with a new one that has the bootstrap token
	// set.
	consulClient, err = api.NewClient(&api.Config{
		Address: firstServerAddr,
		Scheme:  scheme,
		Token:   string(bootstrapToken),
		TLSConfig: api.TLSConfig{
			Address: c.flagConsulTLSServerName,
			CAFile:  c.flagConsulCACert,
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating Consul client for address %s: %s", firstServerAddr, err)
	}

	// Create new tokens for each server and apply them.
	if err := c.setServerTokens(consulClient, serverAddresses, string(bootstrapToken), scheme); err != nil {
		return "", err
	}
	return string(bootstrapToken), nil
}

// setServerTokens creates policies and associated ACL token for each server
// and then provides the token to the server.
func (c *Command) setServerTokens(consulClient *api.Client, serverAddresses []string, bootstrapToken, scheme string) error {
	agentPolicy, err := c.setServerPolicy(consulClient)
	if err != nil {
		return err
	}

	// Create agent token for each server agent.
	for _, host := range serverAddresses {
		var token *api.ACLToken

		// We create a new client for each server because we need to call each
		// server specifically.
		serverClient, err := api.NewClient(&api.Config{
			Address: fmt.Sprintf("%s:%d", host, c.flagServerPort),
			Scheme:  scheme,
			Token:   bootstrapToken,
			TLSConfig: api.TLSConfig{
				Address: c.flagConsulTLSServerName,
				CAFile:  c.flagConsulCACert,
			},
		})

		// Create token for the server
		err = c.untilSucceeds(fmt.Sprintf("creating server token for %s - PUT /v1/acl/token", host),
			func() error {
				tokenReq := api.ACLToken{
					Description: fmt.Sprintf("Server Token for %s", host),
					Policies:    []*api.ACLTokenPolicyLink{{Name: agentPolicy.Name}},
				}
				var err error
				token, _, err = serverClient.ACL().TokenCreate(&tokenReq, nil)
				return err
			})
		if err != nil {
			return err
		}

		// Pass out agent tokens to servers.
		// Update token.
		err = c.untilSucceeds(fmt.Sprintf("updating server token for %s - PUT /v1/agent/token/agent", host),
			func() error {
				_, err := serverClient.Agent().UpdateAgentACLToken(token.SecretID, nil)
				return err
			})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Command) setServerPolicy(consulClient *api.Client) (api.ACLPolicy, error) {
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
			return c.createOrUpdateACLPolicy(agentPolicy, consulClient)
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
