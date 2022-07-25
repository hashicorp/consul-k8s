package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
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

	// annotationInject is the key of the annotation that controls whether
	// injection is explicitly enabled or disabled for a pod.
	annotationInject = "consul.hashicorp.com/connect-inject"
	// annotationCNIProxyConfig stores iptables.Config information so that the CNI plugin can use it to apply
	// iptables rules.
	annotationCNIProxyConfig = "consul.hashicorp.com/cni-proxy-config"
	// retries is the number of backoff retries to attempt while waiting for an cni-proxy-config annotation to
	// poplulate.
	retries = 10
	// dnsServiceHostEnvSuffix is the suffix that is used to get the DNS host IP. The DNS IP is saved as an
	// environment variable with prefix + suffix. The prefix is passed in as cli flag to the endpoints controller
	// which is then passed on in the cni-proxy-config annotation so that the CNI plugin can use it. The DNS
	// environment variable usually looks like: CONSUL_CONSUL_DNS_SERVICE_HOST but the prefix can change
	// depending on the helm install.
	dnsServiceHostEnvSuffix = "DNS_SERVICE_HOST"
)

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
	RuntimeConfig *struct {
		SampleConfig map[string]interface{} `json:"sample_config"`
	} `json:"runtime_config"`

	// Name of the plugin (consul-cni).
	Name string `json:"name"`
	// Type of plugin (consul-cni).
	Type string `json:"type"`
	// CNIBinDir is the location of the cni config files on the node. Can be set as a cli flag.
	CNIBinDir string `json:"cni_bin_dir"`
	// CNINetDir is the locaion of the cni plugin on the node. Can be set as a cli flag.
	CNINetDir string `json:"cni_net_dir"`
	// DNSPrefix is used to determine the Consul Server DNS IP. The IP is set as an environment variable and the
	// prefix allows us
	// to search for it. The DNS IP is determined using the prefix and the dnsServiceHostEnvSuffix constant.
	DNSPrefix string `json:"dns_prefix"`
	// Multus is if the plugin is a multus plugin. Can be set as a cli flag.
	Multus bool `json:"multus"`
	// Kubeconfig file name. Can be set as a cli flag.
	Kubeconfig string `json:"kubeconfig"`
	// LogLevl is the logging level. Can be set as a cli flag.
	LogLevel string `json:"log_level"`
}

// parseConfig parses the supplied CNI configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	cfg := PluginConf{}

	if err := json.Unmarshal(stdin, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	// The previous result is passed from the previously run plugin to our plugin. We do not
	// do anything with the result but instead just pass it on when our plugin is finished.
	if err := version.ParsePrevResult(&cfg.NetConf); err != nil {
		return nil, fmt.Errorf("could not parse prevResult: %v", err)
	}

	// TODO: Do validation of the config that is passed in.

	return &cfg, nil
}

// cmdAdd is called for ADD requests.
func cmdAdd(args *skel.CmdArgs) error {
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
		return fmt.Errorf("failed to convert prevResult: %v", err)
	}

	if len(prevResult.IPs) == 0 {
		return fmt.Errorf("got no container IPs")
	}

	// Pass the prevResult through this plugin to the next one.
	result := prevResult
	logger.Debug("consul-cni previous result", "result", result)

	// Connect to kubernetes.
	ctx := context.Background()
	restConfig, err := clientcmd.BuildConfigFromFlags("", filepath.Join(cfg.CNINetDir, cfg.Kubeconfig))
	if err != nil {
		return fmt.Errorf("could not get rest config from kubernetes api: %s", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("error initializing Kubernetes client: %s", err)
	}

	pod, err := client.CoreV1().Pods(podNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Skip pod that has consul.hashicorp.com/connect-inject: false.
	if skipConnectInject(*pod) {
		logger.Debug("skipping traffic redirect on un-injected pod: %s", pod.Name)
		return types.PrintResult(result, cfg.CNIVersion)
	}

	// Check to see if the cni-proxy-config annotation exists and if not, wait, retry and backoff.
	exists := waitForAnnotation(*pod, annotationCNIProxyConfig, uint64(retries))
	if !exists {
		return fmt.Errorf("could not retrieve annotation: %s on pod: %s", annotationCNIProxyConfig, pod.Name)
	}

	// Parse the cni-proxy-config annotation into an iptables.Config object.
	iptablesCfg, err := parseAnnotation(*pod, annotationCNIProxyConfig)
	if err != nil {
		return fmt.Errorf("could not parse annotation: %s, error: %v", annotationCNIProxyConfig, err)
	}

	// Get the DNS IP from the environment.
	dnsIP := searchDNSIPFromEnvironment(*pod, cfg.DNSPrefix)
	if dnsIP != "" {
		logger.Info("assigned consul DNS IP to %s", dnsIP)
		iptablesCfg.ConsulDNSIP = dnsIP
	}

	// Apply the iptables rules.
	err = iptables.Setup(iptablesCfg)
	if err != nil {
		return fmt.Errorf("could not apply iptables setup: %v", err)
	}

	logger.Debug("traffic redirect rules applied to pod: %s", pod.Name)
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
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("consul-cni"))
}

// skipConnectInject determines if the connect-inject annotation is false.
func skipConnectInject(pod corev1.Pod) bool {
	if anno, ok := pod.Annotations[annotationInject]; ok && anno == "false" {
		return true
	}
	return false
}

// waitForAnnotation waits for an annotation to be available. Returns immediately if the annotation exists.
func waitForAnnotation(pod corev1.Pod, annotation string, retries uint64) bool {
	var err error
	err = backoff.Retry(func() error {
		var ok bool
		_, ok = pod.Annotations[annotation]
		if !ok {
			return fmt.Errorf("annotation %s does not exist yet", annotation)
		}
		return err
	}, backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), retries))
	return err == nil
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

// searchDNSIPFromEnvironment gets the consul server DNS IP from the pods environment variables. The prefix makes
// searching easier return an empty string.
func searchDNSIPFromEnvironment(pod corev1.Pod, prefix string) string {
	var result string
	upcaseResourcePrefix := strings.ToUpper(prefix)
	upcaseResourcePrefixWithUnderscores := strings.ReplaceAll(upcaseResourcePrefix, "-", "_")
	dnsName := strings.Join([]string{upcaseResourcePrefixWithUnderscores, dnsServiceHostEnvSuffix}, "_")

	// Environment variables are buried in the pod spec.
	vars := pod.Spec.Containers[0].Env
	for k := range vars {
		if vars[k].Name == dnsName {
			result = vars[k].Value
			break
		}
	}
	return result
}
