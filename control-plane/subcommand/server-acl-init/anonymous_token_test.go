package serveraclinit

import (
	"strings"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func Test_configureAnonymousPolicy(t *testing.T) {

	k8s, testClient := completeSetup(t)
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
	consul, err := api.NewClient(&api.Config{
		Address: consulHTTPAddr,
		Token:   bootToken,
	})
	require.NoError(t, err)

	// creates new anonymous token policy
	errx := cmd.configureAnonymousPolicy(consul)
	require.NoError(t, errx)

	// does not create/update anonymous token policy
	erry := cmd.configureAnonymousPolicy(consul)
	require.NoError(t, erry)

}
