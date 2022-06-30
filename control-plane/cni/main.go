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
	"github.com/hashicorp/go-hclog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
)

const (
	keyCNIStatus    = "consul.hashicorp.com/cni-status"
	keyInjectStatus = "consul.hashicorp.com/connect-inject-status"
	injected        = "injected"
)

type CNIArgs struct {
	types.CommonArgs
	IP                         net.IP
	K8S_POD_NAME               types.UnmarshallableString
	K8S_POD_NAMESPACE          types.UnmarshallableString
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString
}

// PluginConf is is the configuration used by the plugin.
type PluginConf struct {
	// NetConf is the CNI Specification configuration for standard fields like Name, Type,
	// CNIVersion and PrevResult
	types.NetConf

	RuntimeConfig *struct {
		SampleConfig map[string]interface{} `json:"sample_config"`
	} `json:"runtime_config"`

	Name string `json:"name"`
	// Type of plugin (consul-cni)
	Type string `json:"type"`
	// CNIBinDir is the location of the cni config files on the node. Can bet as a cli flag.
	CNIBinDir string `json:"cni_bin_dir"`
	// CNINetDir is the locaion of the cni plugin on the node. Can be set as a cli flag.
	CNINetDir string `json:"cni_net_dir"`
	// Multus is if the plugin is a multus plugin. Can be set as a cli flag.
	Multus bool `json:"multus"`
	// Kubeconfig file name. Can be set as a cli flag.
	Kubeconfig string `json:"kubeconfig"`
	// LogLevl is the logging level. Can be set as a cli flag.
	LogLevel string `json:"log_level"`
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	cfg := PluginConf{}

	if err := json.Unmarshal(stdin, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	// The previous result is passed from the previously run plugin to our plugin. We do not
	// do anything with the result but instead just pass it on when our plugin is finished
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

	// Get the values of args passed through CNI_ARGS
	cniArgs := CNIArgs{}
	if err := types.LoadArgs(args.Args, &cniArgs); err != nil {
		return err
	}

	podNamespace := string(cniArgs.K8S_POD_NAMESPACE)
	podName := string(cniArgs.K8S_POD_NAME)

	// We only run in a pod
	if podNamespace == "" && podName == "" {
		return fmt.Errorf("not running in a pod, namespace and pod should have values")
	}

	logPrefix := fmt.Sprintf("%s/%s", podNamespace, podName)
	logger := hclog.New(&hclog.LoggerOptions{
		Name:  logPrefix,
		Level: hclog.LevelFromString(cfg.LogLevel),
	})

	// Check to see if the plugin is a chained plugin
	if cfg.PrevResult == nil {
		return fmt.Errorf("must be called as final chained plugin")
	}

	logger.Debug("consul-cni plugin config", "config", cfg)
	// Convert the PrevResult to a concrete Result type that can be modified. The CNI standard says
	// that the previous result needs to be passed onto the next plugin
	prevResult, err := current.GetResult(cfg.PrevResult)
	if err != nil {
		return fmt.Errorf("failed to convert prevResult: %v", err)
	}

	if len(prevResult.IPs) == 0 {
		return fmt.Errorf("got no container IPs")
	}

	// Pass the prevResult through this plugin to the next one
	result := prevResult
	logger.Debug("consul-cni previous result", "result", result)

	ctx := context.Background()
	restConfig, err := clientcmd.BuildConfigFromFlags("", filepath.Join(cfg.CNINetDir, cfg.Kubeconfig))
	if err != nil {
		return fmt.Errorf("could not get rest config from kubernetes api: %s", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("Error initializing Kubernetes client: %s", err)
	}

	pod, err := client.CoreV1().Pods(podNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// TODO: Add tests for all of the logic below
	// TODO: Remove has hasBeenInjected and instead look for consul.hashicorp.com/cni-proxy-config annotation
	// TODO: Add wait and timeout for annotations to show up
	if hasBeenInjected(*pod) {
		// If everything is good, add an annotation to the pod
		// TODO: Remove this as it is just a stub to prove that we can do kubernetes things with the plugin
		annotations := map[string]string{
			keyCNIStatus: "true",
		}
		pod.SetAnnotations(annotations)
		_, err = client.CoreV1().Pods(podNamespace).Update(context.TODO(), pod, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	} else {
		logger.Debug("skipping traffic redirect on un-injected pod")
	}

	// TODO: Get transparent proxy annotations and merge with proxy config
	// TODO: Redirect traffic :)

	// Pass through the result for the next plugin
	return types.PrintResult(result, cfg.CNIVersion)
}

// cmdDel is called for DELETE requests.
func cmdDel(args *skel.CmdArgs) error {
	// Nothing for consul-cni plugin to do as everything is removed once the pod is gone
	return nil
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("consul-cni"))
}

func hasBeenInjected(pod corev1.Pod) bool {
	if anno, ok := pod.Annotations[keyInjectStatus]; ok && anno == injected {
		return true
	}
	return false
}
