package installcni

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	// Default location of the service account token on a kubernetes host.
	defaultTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// createKubeConfig creates the kubeconfig file that the consul-cni plugin will use to communicate with the
// kubernetes API.
func createKubeConfig(cniNetDir, kubeconfigFile string) error {
	var restCfg *rest.Config

	// TODO: Move clientset out of this method and put it in 'Run'

	// Get kube config information from cluster.
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

	token, err := serviceAccountToken(defaultTokenPath)
	if err != nil {
		return err
	}

	data, err := kubeConfigYaml(server, token, restCfg.CAData)
	if err != nil {
		return err
	}

	// Write the kubeconfig file to the host.
	destFile := filepath.Join(cniNetDir, kubeconfigFile)
	err = os.WriteFile(destFile, data, os.FileMode(0o644))
	if err != nil {
		return fmt.Errorf("error writing kube config file %s: %v", destFile, err)
	}

	return nil
}

// kubeConfigYaml creates the kubeconfig in yaml format using kubectl packages.
func kubeConfigYaml(server, token string, certificateAuthorityData []byte) ([]byte, error) {
	// Use the same struct that kubectl uses to create the kubeconfig file.
	kubeconfig := clientcmdapi.Config{
		APIVersion: "v1",
		Kind:       "Config",
		Clusters: map[string]*clientcmdapi.Cluster{
			"local": {
				Server:                   server,
				CertificateAuthorityData: certificateAuthorityData,
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

	// Create yaml from the kubeconfig using the yaml writer from kubectl.
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
		protocol = "https"
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
	server := fmt.Sprintf("%s://[%s]:%s", protocol, host, port)
	return server, nil
}

// serviceAccountToken gets the service token from a directory on the host.
func serviceAccountToken(tokenPath string) (string, error) {
	if _, err := os.Stat(tokenPath); errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("tokenPath does not exist: %v", err)
	}
	token, err := ioutil.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("could not read service account token: %v", err)
	}
	return string(token), nil
}
