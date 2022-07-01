package installcni

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// createKubeConfig creates the kubeconfig file that the consul-cni plugin will use to communicate with the
// kubernetes API.
func createKubeConfig(mountedPath, kubeconfigFile string, logger hclog.Logger) error {
	var restCfg *rest.Config

	// Get kube config information from cluster
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	err = rest.LoadTLSFiles(restCfg)
	if err != nil {
		return err
	}

	server, err := kubernetesServer()
	if err != nil {
		return err
	}

	ca, err := certificatAuthority(restCfg.CAData)
	if err != nil {
		return err
	}

	token, err := serviceAccountToken()
	if err != nil {
		return err
	}

	data, err := kubeConfigYaml(server, token, ca)
	if err != nil {
		return err
	}

	// Write the kubeconfig file to the host
	destFile := filepath.Join(mountedPath, kubeconfigFile)
	err = os.WriteFile(destFile, data, os.FileMode(0o644))
	if err != nil {
		return fmt.Errorf("error writing kube config file %s: %v", destFile, err)
	}

	logger.Info("Wrote kubeconfig file", "name", destFile)
	return nil
}

// kubeConfigYaml creates the kubeconfig in yaml format using kubectl packages.
func kubeConfigYaml(server, token, certificateAuthority string) ([]byte, error) {
	// Use the same struct that kubectl uses to create the kubeconfig file
	kubeconfig := clientcmdapi.Config{
		APIVersion: "v1",
		Kind:       "Config",
		Clusters: map[string]*clientcmdapi.Cluster{
			"local": {
				Server:               server,
				CertificateAuthority: certificateAuthority,
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"consul-cni": {
				Token: token,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"consul-cni-context": {
				Cluster:  "local",
				AuthInfo: "consul-cni",
			},
		},
		CurrentContext: "consul-cni-context",
	}

	// Create yaml from the kubeconfig using kubectls yaml writer
	data, err := clientcmd.Write(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("error creating kubeconfig yaml: %v", err)
	}
	return data, nil
}

// kubernetesServer gets the protocol, host and port from the server environment.
func kubernetesServer() (string, error) {
	protocol, ok := os.LookupEnv("KUBERNETES_SERVICE_PROTOCOL")
	if !ok {
		return "", fmt.Errorf("Unable to get kubernetes api server protocol from environment")
	}

	host, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	if !ok {
		return "", fmt.Errorf("Unable to get kubernetes api server host from environment")
	}

	port, ok := os.LookupEnv("KUBERNETES_SERVICE_PORT")
	if !ok {
		return "", fmt.Errorf("Unable to get kubernetes api server port from environment")
	}

	// Server string format is https://[127.0.0.1]:443. The [] are what other plugins are using in their kubeconfig.
	server := fmt.Sprintf("%s//[%s]:%s", protocol, host, port)
	return server, nil
}

// certificatAuthority gets the certificate authority from the caData.
func certificatAuthority(caData []byte) (string, error) {
	if len(caData) == 0 {
		return "", fmt.Errorf("Empty certificate authority returned from kubernetes rest api")
	}
	return base64.StdEncoding.EncodeToString(caData), nil
}

// serviceAccountToken gets the service token from a directory on the host.
func serviceAccountToken() (string, error) {
	token, err := ioutil.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("could not read service account token: %v", err)
	}
	return string(token), nil
}
