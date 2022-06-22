package installcni

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"text/template"

	"github.com/hashicorp/go-hclog"
	"k8s.io/client-go/rest"
)

type KubeConfigFields struct {
	KubernetesServiceProtocol string
	KubernetesServiceHost     string
	KubernetesServicePort     string
	TLSConfig                 string
	ServiceAccountToken       string
}

// createKubeConfig creates the kubeconfig file that the consul-cni plugin will use to communicate with the
// kubernetes API.
func createKubeConfig(mountedPath, kubeconfigFile string, logger hclog.Logger) error {

	var kubecfg *rest.Config

	// Get kube config information from cluster
	kubecfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	err = rest.LoadTLSFiles(kubecfg)
	if err != nil {
		return err
	}

	// Get the host, port and protocol used to talk to the kube API
	kubeFields, err := KubernetesFields(kubecfg.CAData, logger)
	if err != nil {
		return err
	}

	// Write out the kubeconfig file
	destFile := filepath.Join(mountedPath, kubeconfigFile)
	err = writeKubeConfig(kubeFields, destFile, logger)
	if err != nil {
		return err
	}

	logger.Info("Wrote kubeconfig file", "name", destFile)
	return nil
}

// KubernetesFields gets the needed fields from the in cluster config.
func KubernetesFields(caData []byte, logger hclog.Logger) (*KubeConfigFields, error) {

	var protocol = "https"
	if val, ok := os.LookupEnv("KUBERNETES_SERVICE_PROTOCOL"); ok {
		protocol = val
	}

	var serviceHost string
	if val, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST"); ok {
		serviceHost = val
	}

	var servicePort string
	if val, ok := os.LookupEnv("KUBERNETES_SERVICE_PORT"); ok {
		servicePort = val
	}

	ca := "certificate-authority-data: " + base64.StdEncoding.EncodeToString(caData)

	serviceToken, err := ServiceAccountToken()
	if err != nil {
		return nil, err
	}

	logger.Debug("KubernetesFields: got fields", "protocol", protocol, "kubernetes host", serviceHost, "kubernetes port", servicePort)
	return &KubeConfigFields{
		KubernetesServiceProtocol: protocol,
		KubernetesServiceHost:     serviceHost,
		KubernetesServicePort:     servicePort,
		TLSConfig:                 ca,
		ServiceAccountToken:       serviceToken,
	}, nil
}

// ServiceAccountToken gets the service token from a directory on the host.
func ServiceAccountToken() (string, error) {
	// serviceAccounttoken = /var/run/secrets/kubernetes.io/serviceaccount/token
	token, err := ioutil.ReadFile(serviceAccountToken)
	if err != nil {
		return "", fmt.Errorf("could not read service account token: %v", err)
	}
	return string(token), nil

}

// writeKubeConfig writes out the kubeconfig file using a template.
func writeKubeConfig(fields *KubeConfigFields, destFile string, logger hclog.Logger) error {

	tmpl, err := template.New("kubeconfig").Parse(kubeconfigTmpl)
	if err != nil {
		return fmt.Errorf("could not parse kube config template: %v", err)
	}

	var templateBuffer bytes.Buffer
	if err := tmpl.Execute(&templateBuffer, fields); err != nil {
		return fmt.Errorf("could not execute kube config template: %v", err)
	}

	err = os.WriteFile(destFile, templateBuffer.Bytes(), os.FileMode(0o644))
	if err != nil {
		return fmt.Errorf("error writing kube config file %s: %v", destFile, err)
	}

	logger.Debug("writeKubeConfig:", "destFile", destFile)
	return nil
}

const (
	serviceAccountToken = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	kubeconfigTmpl      = `# Kubeconfig file for consul CNI plugin.
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: {{.KubernetesServiceProtocol}}://[{{.KubernetesServiceHost}}]:{{.KubernetesServicePort}}
    {{.TLSConfig}}
users:
- name: consul-cni
  user:
    token: "{{.ServiceAccountToken}}"
contexts:
- name: consul-cni-context
  context:
    cluster: local
    user: consul-cni
current-context: consul-cni-context
`
)
