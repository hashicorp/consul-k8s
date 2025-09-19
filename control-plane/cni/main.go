// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	cniv "github.com/containernetworking/cni/pkg/version"
	"github.com/hashicorp/consul-k8s/version"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/hashicorp/go-hclog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
)

const (
	// These annotations are duplicated from control-plane/connect-inject/annotations_and_labels.go in
	// order to prevent pulling in dependencies.

	// keyInjectStatus is the key of the annotation that is added to
	// a pod after an injection is done.
	keyInjectStatus = "consul.hashicorp.com/connect-inject-status"

	// keyMeshInjectStatus is the mesh v2 key of the annotation that is added to
	// a pod after an injection is done.
	keyMeshInjectStatus = "consul.hashicorp.com/mesh-inject-status"

	// keyTransparentProxyStatus is the key of the annotation that is added to
	// a pod when transparent proxy is done.
	keyTransparentProxyStatus = "consul.hashicorp.com/transparent-proxy-status"

	// waiting is used in conjunction with keyTransparentProxyStatus as a simple way to
	// indicate the status of the CNI plugin.
	waiting = "waiting"

	// complete is used in conjunction with keyTransparentProxyStatus as a simple way to
	// indicate the status of the CNI plugin.
	complete = "complete"

	// annotationRedirectTraffic stores iptables.Config information so that the CNI plugin can use it to apply
	// iptables rules.
	annotationRedirectTraffic = "consul.hashicorp.com/redirect-traffic-config"
)

type Command struct {
	// client is a kubernetes client
	client kubernetes.Interface
	// iptablesProvider is the Provider that will apply iptables rules. Used for testing.
	iptablesProvider iptables.Provider
}

type CNIArgs struct {
	// types.CommonArgs are args that are passed as part of the CNI standard.
	types.CommonArgs
	// IP address assigned to the pod from a previous plugin.
	IP net.IP
	// K8S_POD_NAME is the pod that the plugin is running for.
	K8S_POD_NAME types.UnmarshallableString
	// K8S_POD_NAMESPACE is the namespace that the plugin is running for.
	K8S_POD_NAMESPACE types.UnmarshallableString
	// K8S_POD_INFRA_CONTAINER_ID is the runtime container ID that the pod runs under.
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString

	// CONSUL_IPTABLES_CONFIG is the runtime iptables configuration passed by
	// orchestrator (ex. the Nomad client agent)
	CONSUL_IPTABLES_CONFIG types.UnmarshallableString
}

// PluginConf is the configuration used by the plugin.
type PluginConf struct {
	// NetConf is the CNI Specification configuration for standard fields like Name, Type,
	// CNIVersion and PrevResult.
	types.NetConf

	// RuntimeConfig is the config passed from the kubelet to plugin at runtime.
	RuntimeConfig *struct{} `json:"runtime_config"`

	// Name of the plugin (consul-cni).
	Name string `json:"name"`
	// Type of plugin (consul-cni).
	Type string `json:"type"`
	// CNIBinDir is the location of the cni config files on the node. Can be set as a cli flag.
	CNIBinDir string `json:"cni_bin_dir"`
	// CNINetDir is the location of the cni plugin on the node. Can be set as a cli flag.
	CNINetDir string `json:"cni_net_dir"`
	// Multus is if the plugin is a multus plugin. Can be set as a cli flag.
	Multus bool `json:"multus"`
	// Kubeconfig file name. Can be set as a cli flag.
	Kubeconfig string `json:"kubeconfig"`
	// LogLevel is the logging level. Can be set as a cli flag.
	LogLevel string `json:"log_level"`
}

// parseConfig parses the supplied CNI configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	cfg := PluginConf{}

	if err := json.Unmarshal(stdin, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %w", err)
	}

	// The previous result is passed from the previously run plugin to our plugin. We do not
	// do anything with the result but instead just pass it on when our plugin is finished.
	if err := cniv.ParsePrevResult(&cfg.NetConf); err != nil {
		return nil, fmt.Errorf("could not parse prevResult: %w", err)
	}

	return &cfg, nil
}

// cmdAdd is called for ADD requests.
func (c *Command) cmdAdd(args *skel.CmdArgs) error {
	cfg, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}

	// Get the values of args passed through CNI_ARGS.
	cniArgs := CNIArgs{}
	if err := types.LoadArgs(args.Args, &cniArgs); err != nil {
		return err
	}

	podNamespace := string(cniArgs.K8S_POD_NAMESPACE)
	podName := string(cniArgs.K8S_POD_NAME)
	cniArgsIPTablesCfg := string(cniArgs.CONSUL_IPTABLES_CONFIG)

	// We should never encounter this unless there has been an error in the
	// kubelet. A good safeguard.
	if (podNamespace == "" || podName == "") && cniArgsIPTablesCfg == "" {
		return fmt.Errorf("not running in a pod, namespace and pod should have values")
	}

	logPrefix := fmt.Sprintf("%s/%s", podNamespace, podName)
	logger := hclog.New(&hclog.LoggerOptions{
		Name:  logPrefix,
		Level: hclog.LevelFromString(cfg.LogLevel),
	})

	logger.Debug("consul-cni plugin config", "config", cfg)

	// Only chained plugins have a previous result.
	var result *current.Result

	// Check to see if the plugin is a chained plugin.
	if cfg.PrevResult == nil {
		// The plugin is not chained (ie multus) so create a fake result
		result = &current.Result{
			CNIVersion: "0.3.1",
		}
	} else {
		prevResult, err := current.GetResult(cfg.PrevResult)
		if err != nil {
			return fmt.Errorf("failed to convert prevResult: %w", err)
		}
		if len(prevResult.IPs) == 0 {
			return fmt.Errorf("got no container IPs")
		}
		// Pass the prevResult through this plugin to the next one.
		result = prevResult
	}

	var iptablesCfg iptables.Config

	// If cniArgsIPTablesCfg is populated we're on Nomad, otherwise we're on K8s
	if cniArgsIPTablesCfg != "" {
		var err error
		iptablesCfg, err = parseIPTablesFromCNIArgs(cniArgsIPTablesCfg)
		if err != nil {
			return err
		}
	} else {
		if c.client == nil {
			if err := c.createK8sClient(cfg, logger); err != nil {
				return err
			}
		}

		ctx := context.Background()
		pod, err := c.client.CoreV1().Pods(podNamespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error retrieving pod: %s", err)
		}

		// Skip traffic redirection if the correct annotations are not on the pod.
		if skipTrafficRedirection(*pod) {
			logger.Debug("skipping traffic redirection because the pod is either not injected or transparent proxy is disabled: %s", pod.Name)
			return types.PrintResult(result, cfg.CNIVersion)
		}

		// We do not throw an error here because kubernetes will often throw a
		// benign error where the pod has been updated in between the get and
		// update of the annotation. Eventually kubernetes will update the
		// annotation
		ok := c.updateTransparentProxyStatusAnnotation(podName, podNamespace, waiting)
		if !ok {
			logger.Info("unable to update %s pod annotation to waiting", keyTransparentProxyStatus)
		}

		// Parse the cni-proxy-config annotation into an iptables.Config object.
		iptablesCfg, err = parseAnnotation(*pod, annotationRedirectTraffic)
		if err != nil {
			return err
		}
	}

	// Set NetNS passed through the CNI.
	iptablesCfg.NetNS = args.Netns

	// Set the provider to a fake provider in testing, otherwise use the default
	// iptables.Provider
	if c.iptablesProvider != nil {
		iptablesCfg.IptablesProvider = c.iptablesProvider
	}

	dualStack := false
	if os.Getenv("CNI_DUAL_STACK") == "true" {
		dualStack = true
	}

	// Apply the iptables rules.
	err = iptables.Setup(iptablesCfg, dualStack)
	if err != nil {
		return fmt.Errorf("could not apply iptables setup: %v", err)
	}

	if cniArgsIPTablesCfg == "" {
		// We do not throw an error here because kubernetes will often throw a
		// benign error where the pod has been updated in between the get and update
		// of the annotation. Eventually kubernetes will update the annotation
		ok := c.updateTransparentProxyStatusAnnotation(podName, podNamespace, complete)
		if !ok {
			logger.Info("unable to update %s pod annotation to complete", keyTransparentProxyStatus)
		}
	}

	logger.Debug("traffic redirect rules applied to pod: %s", podName)

	// Pass through the result for the next plugin even if we are the final
	// plugin in the chain.
	return types.PrintResult(result, cfg.CNIVersion)
}

// cmdDel is called for DELETE requests.
func cmdDel(_ *skel.CmdArgs) error {
	// Nothing to do but this function will still be called as part of the CNI specification.
	return nil
}

// cmdCheck is called for CHECK requests.
func cmdCheck(_ *skel.CmdArgs) error {
	// Nothing to do but this function will still be called as part of the CNI specification.
	return nil
}

func main() {
	c := &Command{}
	bv.BuildVersion = version.GetHumanVersion()
	skel.PluginMain(c.cmdAdd, cmdCheck, cmdDel, cniv.All, bv.BuildString("consul-cni"))
}

// createK8sClient configures the command's Kubernetes API client if it doesn't
// already exist.
// TODO: remove logger for auth provider details
func (c *Command) createK8sClient(cfg *PluginConf, logger hclog.Logger) error {
	restConfig, err := clientcmd.BuildConfigFromFlags("", filepath.Join(cfg.CNINetDir, cfg.Kubeconfig))
	logger.Info("tokenFile used  - ", restConfig.BearerTokenFile)
	if err != nil {
		return fmt.Errorf("could not get rest config from kubernetes api: %s", err)
	}
	c.client, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("error initializing Kubernetes client: %s", err)
	}
	return nil
}

// skipTrafficRedirection looks for annotations on the pod and determines if it should skip traffic redirection.
// The absence of the annotations is the equivalent of "disabled" because it means that the connect-inject mutating
// webhook did not run against the pod.
func skipTrafficRedirection(pod corev1.Pod) bool {
	// If keyInjectStatus exists, then we are dealing with a mesh v1 pod
	// else we have a mesh v2 pod. We need to check for both before we can skip.
	if anno, ok := pod.Annotations[keyInjectStatus]; !ok || anno == "" {
		if anno, ok := pod.Annotations[keyMeshInjectStatus]; !ok || anno == "" {
			return true
		}
	}

	if anno, ok := pod.Annotations[keyTransparentProxyStatus]; !ok || anno == "" {
		return true
	}
	return false
}

func parseIPTablesFromCNIArgs(args string) (iptables.Config, error) {
	cfg := iptables.Config{}
	err := json.Unmarshal([]byte(args), &cfg)
	if err != nil {
		return cfg, fmt.Errorf("could not unmarshal CNI args: %w", err)
	}
	return cfg, nil
}

// parseAnnotation parses the cni-proxy-config annotation into an iptables.Config object.
func parseAnnotation(pod corev1.Pod, annotation string) (iptables.Config, error) {
	anno, ok := pod.Annotations[annotation]
	if !ok {
		return iptables.Config{}, fmt.Errorf("could not find %s annotation for %s pod", annotation, pod.Name)
	}
	cfg := iptables.Config{}
	err := json.Unmarshal([]byte(anno), &cfg)
	if err != nil {
		return iptables.Config{}, fmt.Errorf("could not unmarshal %s annotation for %s pod", annotation, pod.Name)
	}
	return cfg, nil
}

// updateTransparentProxyStatusAnnotation updates the transparent-proxy-status annotation. We use it as a simple inicator of
// CNI status on the pod.  Failing is not fatal.
func (c *Command) updateTransparentProxyStatusAnnotation(podName, namespace, status string) bool {
	// Refresh the pod so that we can update it without problems
	pod, err := c.client.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return false
	}
	pod.Annotations[keyTransparentProxyStatus] = status
	_, err = c.client.CoreV1().Pods(namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
	return err == nil
}
