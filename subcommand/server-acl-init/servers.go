package serveraclinit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/consul/api"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// podAddr is a convenience struct for passing around pod names and
// addresses for Consul servers.
type podAddr struct {
	// Name is the name of the pod.
	Name string
	// Addr is in the form "<ip>:<port>".
	Addr string
}

// getConsulServers returns n Consul server pods with their http addresses.
// If there are less server pods than 'n' then the function will wait.
func (c *Command) getConsulServers(n int, scheme string) ([]podAddr, error) {
	var serverPods *apiv1.PodList
	err := c.untilSucceeds("discovering Consul server pods",
		func() error {
			var err error
			serverPods, err = c.clientset.CoreV1().Pods(c.flagK8sNamespace).List(metav1.ListOptions{LabelSelector: c.flagServerLabelSelector})
			if err != nil {
				return err
			}

			if len(serverPods.Items) == 0 {
				return fmt.Errorf("no server pods with labels %q found", c.flagServerLabelSelector)
			}

			if len(serverPods.Items) < n {
				return fmt.Errorf("found %d servers, require %d", len(serverPods.Items), n)
			}

			for _, pod := range serverPods.Items {
				if pod.Status.PodIP == "" {
					return fmt.Errorf("pod %s has no IP", pod.Name)
				}
			}
			return nil
		})
	if err != nil {
		return nil, err
	}

	var podAddrs []podAddr
	for _, pod := range serverPods.Items {
		var httpPort int32
		for _, p := range pod.Spec.Containers[0].Ports {
			if p.Name == scheme {
				httpPort = p.ContainerPort
			}
		}
		if httpPort == 0 {
			return nil, fmt.Errorf("pod %s has no port labeled '%s'", pod.Name, scheme)
		}
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, httpPort)
		podAddrs = append(podAddrs, podAddr{
			Name: pod.Name,
			Addr: addr,
		})
	}
	return podAddrs, nil
}

// bootstrapServers bootstraps ACLs and ensures each server has an ACL token.
func (c *Command) bootstrapServers(bootTokenSecretName, scheme string) (string, error) {
	serverPods, err := c.getConsulServers(c.flagReplicas, scheme)
	if err != nil {
		return "", err
	}
	c.Log.Info(fmt.Sprintf("Found %d Consul server Pods", len(serverPods)))

	// Pick the first pod to connect to for bootstrapping and set up connection.
	firstServerAddr := serverPods[0].Addr
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
					"token": bootstrapToken,
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
	if err := c.setServerTokens(consulClient, serverPods, string(bootstrapToken), scheme); err != nil {
		return "", err
	}
	return string(bootstrapToken), nil
}

// setServerTokens creates policies and associated ACL token for each server
// and then provides the token to the server.
func (c *Command) setServerTokens(consulClient *api.Client,
	serverPods []podAddr, bootstrapToken, scheme string) error {

	agentPolicy, err := c.setServerPolicy(consulClient)
	if err != nil {
		return err
	}

	// Create agent token for each server agent.
	var serverTokens []api.ACLToken
	for _, pod := range serverPods {
		var token *api.ACLToken
		err := c.untilSucceeds(fmt.Sprintf("creating server token for %s - PUT /v1/acl/token", pod.Name),
			func() error {
				tokenReq := api.ACLToken{
					Description: fmt.Sprintf("Server Token for %s", pod.Name),
					Policies:    []*api.ACLTokenPolicyLink{{Name: agentPolicy.Name}},
				}
				var err error
				token, _, err = consulClient.ACL().TokenCreate(&tokenReq, nil)
				return err
			})
		if err != nil {
			return err
		}
		serverTokens = append(serverTokens, *token)
	}

	// Pass out agent tokens to servers.
	for i, pod := range serverPods {
		// We create a new client for each server because we need to call each
		// server specifically.
		serverClient, err := api.NewClient(&api.Config{
			Address: pod.Addr,
			Scheme:  scheme,
			Token:   bootstrapToken,
			TLSConfig: api.TLSConfig{
				Address: c.flagConsulTLSServerName,
				CAFile:  c.flagConsulCACert,
			},
		})
		if err != nil {
			return fmt.Errorf(" creating Consul client for address %q: %s", pod.Addr, err)
		}
		podName := pod.Name

		// Update token.
		err = c.untilSucceeds(fmt.Sprintf("updating server token for %s - PUT /v1/agent/token/agent", podName),
			func() error {
				_, err := serverClient.Agent().UpdateAgentACLToken(serverTokens[i].SecretID, nil)
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
		c.Log.Error("Error templating server agent rules", "err", err)
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
