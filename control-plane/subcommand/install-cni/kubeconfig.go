// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package installcni

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/consul-k8s/control-plane/cni/config"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type TokenInfo struct {
	TokenInfoType TokenInfoType
	TokenInfo     string
}

type TokenInfoType string

const (
	TokenTypeFile TokenInfoType = "TokenFile"
	TokenTypeRaw  TokenInfoType = "Token"
)

// createKubeConfig creates the kubeconfig file that the consul-cni plugin will use to communicate with the
// kubernetes API.
func createKubeConfig(destDir string, cfg *config.CNIConfig) error {
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

	tokenInfo := TokenInfo{}
	if cfg.AutorotateToken {
		tokenInfo.TokenInfoType = TokenTypeFile
		tokenInfo.TokenInfo = cfg.CNIHostTokenPath
	} else {
		// This is the older flow which runs in the cni pod so
		// this will find token on the default CNITokenPath at the time of creation of kubeconfig
		token, err := serviceAccountToken(cfg.CNITokenPath)
		if err != nil {
			return err
		}
		tokenInfo.TokenInfoType = TokenTypeRaw
		tokenInfo.TokenInfo = token
	}

	data, err := kubeConfigYaml(server, &tokenInfo, restCfg.CAData)
	if err != nil {
		return err
	}

	// Write the kubeconfig file to the host.
	destFile := filepath.Join(destDir, cfg.Kubeconfig)
	err = os.WriteFile(destFile, data, os.FileMode(0o644))
	if err != nil {
		return fmt.Errorf("error writing kube config file %s: %w", destFile, err)
	}

	return nil
}

// kubeConfigYaml creates the kubeconfig in yaml format using kubectl packages.
func kubeConfigYaml(server string, tokenInfo *TokenInfo, certificateAuthorityData []byte) ([]byte, error) {
	// Use the same struct that kubectl uses to create the kubeconfig file.
	var consulCNIAuthInfo *clientcmdapi.AuthInfo
	if tokenInfo.TokenInfoType == TokenTypeFile {
		consulCNIAuthInfo = &clientcmdapi.AuthInfo{
			TokenFile: tokenInfo.TokenInfo,
		}
	} else if tokenInfo.TokenInfoType == TokenTypeRaw {
		consulCNIAuthInfo = &clientcmdapi.AuthInfo{
			Token: tokenInfo.TokenInfo,
		}
	}
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
			"consul-cni": consulCNIAuthInfo,
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
		return nil, fmt.Errorf("error creating kubeconfig yaml: %w", err)
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
		return "", fmt.Errorf("tokenPath does not exist: %w", err)
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("could not read service account token: %w", err)
	}
	return string(token), nil
}
