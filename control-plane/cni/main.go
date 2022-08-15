package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/hashicorp/go-hclog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
)

const (
	// These annotations are duplicated from control-plane/connect-inject/annotations.go in
	// order to prevent pulling in dependencies.

	// keyInjectStatus is the key of the annotation that is added to
	// a pod after an injection is done.
	keyInjectStatus = "consul.hashicorp.com/connect-inject-status"

	// keyTransparentProxyStatus is the key of the annotation that is added to
	// a pod when transparent proxy is done.
	keyTransparentProxyStatus = "consul.hashicorp.com/transparent-proxy-status"

	// waiting is used in conjunction with keyTransparentProxyStatus as a simple way to
	// indicate the status of the CNI plugin.
	waiting = "waiting"

	// complete is used in conjunction with keyTransparentProxyStatus as a simple way to
	// indicate the status of the CNI plugin.
	complete = "complete"

	// annotationTrafficRedirection stores iptables.Config information so that the CNI plugin can use it to apply
	// iptables rules.
	annotationTrafficRedirection = "consul.hashicorp.com/traffic-redirection-config"
)

type Command struct {
	client kubernetes.Interface
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
}

// PluginConf is is the configuration used by the plugin.
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
	// LogLevl is the logging level. Can be set as a cli flag.
	LogLevel string `json:"log_level"`
	//
}

// parseConfig parses the supplied CNI configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	cfg := PluginConf{}

	if err := json.Unmarshal(stdin, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %w", err)
	}

	// The previous result is passed from the previously run plugin to our plugin. We do not
	// do anything with the result but instead just pass it on when our plugin is finished.
	if err := version.ParsePrevResult(&cfg.NetConf); err != nil {
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

	// We should never encounter this unless there has been an error in the kubelet. A good safeguard.
	if podNamespace == "" || podName == "" {
		return fmt.Errorf("not running in a pod, namespace and pod should have values")
	}

	logPrefix := fmt.Sprintf("%s/%s", podNamespace, podName)
	logger := hclog.New(&hclog.LoggerOptions{
		Name:  logPrefix,
		Level: hclog.LevelFromString(cfg.LogLevel),
	})

	// Check to see if the plugin is a chained plugin.
	if cfg.PrevResult == nil {
		return fmt.Errorf("must be called as final chained plugin")
	}

	logger.Debug("consul-cni plugin config", "config", cfg)
	// Convert the PrevResult to a concrete Result type that can be modified. The CNI standard says
	// that the previous result needs to be passed onto the next plugin.
	prevResult, err := current.GetResult(cfg.PrevResult)
	if err != nil {
		return fmt.Errorf("failed to convert prevResult: %w", err)
	}

	if len(prevResult.IPs) == 0 {
		return fmt.Errorf("got no container IPs")
	}

	// Pass the prevResult through this plugin to the next one.
	result := prevResult
	logger.Debug("consul-cni previous result", "result", result)

	ctx := context.Background()
	if c.client == nil {

		// Connect to kubernetes.
		restConfig, err := clientcmd.BuildConfigFromFlags("", filepath.Join(cfg.CNINetDir, cfg.Kubeconfig))
		if err != nil {
			return fmt.Errorf("could not get rest config from kubernetes api: %s", err)
		}

		c.client, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			return fmt.Errorf("error initializing Kubernetes client: %s", err)
		}
	}

	pod, err := c.client.CoreV1().Pods(podNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error retrieving pod: %s", err)
	}

	// Skip traffic redirection if the correct annotations are not on the pod.
	if skipTrafficRedirection(*pod) {
		logger.Debug("skipping traffic redirect on un-injected pod: %s", pod.Name)
		return types.PrintResult(result, cfg.CNIVersion)
	}

	err = c.updateTransparentProxyStatusAnnotation(pod, podNamespace, waiting)
	if err != nil {
		return fmt.Errorf("error adding waiting annotation: %s", err)
	}

	// TODO: Insert redirect here

	err = c.updateTransparentProxyStatusAnnotation(pod, podNamespace, complete)
	if err != nil {
		return fmt.Errorf("error adding complete annotation: %s", err)
	}

	// Pass through the result for the next plugin even though we are the final plugin in the chain.
	return types.PrintResult(result, cfg.CNIVersion)
}

// cmdDel is called for DELETE requests.
func cmdDel(args *skel.CmdArgs) error {
	// Nothing to do but this function will still be called as part of the CNI specification.
	return nil
}

// cmdCheck is called for CHECK requests.
func cmdCheck(args *skel.CmdArgs) error {
	// Nothing to do but this function will still be called as part of the CNI specification.
	return nil
}

func main() {
	c := &Command{}
	skel.PluginMain(c.cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("consul-cni"))
}

// skipTrafficRedirection looks for annotations on the pod and determines if it should skip traffic redirection.
// The absence of the annotations is the equivalent of "disabled" because it means that the connect inject mutating
// webhook did not run against the pod.
func skipTrafficRedirection(pod corev1.Pod) bool {
	if anno, ok := pod.Annotations[keyInjectStatus]; !ok || anno == "" {
		return true
	}

	if anno, ok := pod.Annotations[keyTransparentProxyStatus]; !ok || anno == "" {
		return true
	}
	return false
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
// CNI status on the pod.
func (c *Command) updateTransparentProxyStatusAnnotation(pod *corev1.Pod, namespace, status string) error {
	pod.Annotations[keyTransparentProxyStatus] = status
	_, err := c.client.CoreV1().Pods(namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error adding annotation to pod: %s", err)
	}

	return nil
}
