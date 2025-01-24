// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func Test_configureAnonymousPolicy(t *testing.T) {

	k8s, testClient := completeSetup(t, false)
	consulHTTPAddr := testClient.TestServer.HTTPAddr
	consulGRPCAddr := testClient.TestServer.GRPCAddr

	setUpK8sServiceAccount(t, k8s, ns)
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	flags := []string{"-connect-inject"}
	cmdArgs := append([]string{
		"-timeout=1m",
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-auth-method-host=https://my-kube.com",
		"-addresses", strings.Split(consulHTTPAddr, ":")[0],
		"-http-port", strings.Split(consulHTTPAddr, ":")[1],
		"-grpc-port", strings.Split(consulGRPCAddr, ":")[1],
	}, flags...)
	responseCode := cmd.Run(cmdArgs)
	require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

	bootToken := getBootToken(t, k8s, resourcePrefix, ns)
	client, err := consul.NewDynamicClient(&api.Config{
		Address: consulHTTPAddr,
		Token:   bootToken,
	})
	require.NoError(t, err)

	err = cmd.configureAnonymousPolicy(client)
	require.NoError(t, err)

	policy, _, err := client.ConsulClient.ACL().PolicyReadByName(anonymousTokenPolicyName, nil)
	require.NoError(t, err)

	testPolicy := api.ACLPolicy{
		ID:          policy.ID,
		Name:        anonymousTokenPolicyName,
		Description: "Anonymous token Policy",
		Rules:       `acl = "read"`,
	}
	readOnlyPolicy, _, err := client.ConsulClient.ACL().PolicyUpdate(&testPolicy, &api.WriteOptions{})
	require.NoError(t, err)

	err = cmd.configureAnonymousPolicy(client)
	require.NoError(t, err)

	actualPolicy, _, err := client.ConsulClient.ACL().PolicyReadByName(anonymousTokenPolicyName, nil)
	require.NoError(t, err)

	// assert policy is still same.
	require.Equal(t, readOnlyPolicy, actualPolicy)
}
