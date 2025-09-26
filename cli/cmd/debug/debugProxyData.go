// https://www.envoyproxy.io/docs/envoy/latest/operations/admin

package debug

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/envoy"
	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// captureEnvoyProxyData -
// captures consul-k8s Envoy admin endpoint data (/stats, /clusters, /listeners, /config_dump)
// for ALL proxy pods in ALL namespaces and writes it to /proxy dir within debug bundle
func (c *DebugCommand) captureEnvoyProxyData() error {
	// get all proxy pods
	pods, err := c.getEnvoyProxyPodsList()
	if err != nil {
		if err == notFoundError {
			return err
		}
		return fmt.Errorf("error fetching pods list: %s", err)
	}
	// write envoy proxy pods list to json file within debug bundle
	err = c.writeEnvoyProxyPodList(pods)
	if err != nil {
		return err
	}

	// capture all proxy's details and write them to debug bundle
	var errs *multierror.Error
	for _, pod := range pods {
		podProxyType := c.getPodProxyType(pod)
		if err := c.captureEnvoyProxyPodData(pod, podProxyType, pod.Namespace); err != nil {
			err = fmt.Errorf("%s: %v\n", pod.Name, err)
			errs = multierror.Append(errs, err)
		}
	}
	// If any errors were collected during the capture, write them to a file in the debug directory.
	if errs.ErrorOrNil() != nil {
		errorFilePath := filepath.Join(c.output, "proxy", "proxyCaptureErrors.txt")
		errorContent := []byte(errs.Error())
		if err := os.WriteFile(errorFilePath, errorContent, 0644); err != nil {
			return fmt.Errorf("error writing proxy data capture errors to file: %v\n Collected Errors:\n%v", err, errorContent)
		}
		return fmt.Errorf("one or more errors occurred during proxy data collection; \n\tPlease check logs/logCaptureErrors.txt in debug archive for details")
	}
	return nil
}

// getEnvoyProxyPodsList - captures all pods in ALL Namespaces which run envoy proxies,
// making sure to return each pod only once even if multiple label selectors may return the same pod.
func (c *DebugCommand) getEnvoyProxyPodsList() ([]v1.Pod, error) {
	uniquePods := make(map[types.NamespacedName]v1.Pod)
	proxySelectors := []string{
		"component=api-gateway, gateway.consul.hashicorp.com/managed=true",
		"api-gateway.consul.hashicorp.com/managed=true", // Legacy api gateway
		"component=ingress-gateway, chart=consul-helm",
		"component=mesh-gateway, chart=consul-helm",
		"component=terminating-gateway, chart=consul-helm",
		"consul.hashicorp.com/connect-inject-status=injected",
	}
	for _, selector := range proxySelectors {
		pods, err := c.kubernetes.CoreV1().Pods("").List(c.Ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			return nil, err
		}
		// Add pods to the map, which handles uniqueness automatically
		for _, pod := range pods.Items {
			name := types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}
			uniquePods[name] = pod
		}
	}
	if len(uniquePods) == 0 {
		return nil, notFoundError
	}
	// Convert the map values back into a slice
	var allPods []v1.Pod
	for _, pod := range uniquePods {
		allPods = append(allPods, pod)
	}
	return allPods, nil
}

// getProxyType - returns the proxy type of a pod.
func (c *DebugCommand) getPodProxyType(pod v1.Pod) string {
	componentTypeMap := map[string]string{
		"api-gateway":         "API Gateway",
		"ingress-gateway":     "Ingress Gateway",
		"mesh-gateway":        "Mesh Gateway",
		"terminating-gateway": "Terminating Gateway",
	}
	proxyType := "Sidecar"

	if mappedType, ok := componentTypeMap[pod.Labels["component"]]; ok {
		proxyType = mappedType
	} else if pod.Labels["api-gateway.consul.hashicorp.com/managed"] == "true" {
		// Special case for deprecated API Gateway.
		proxyType = "API Gateway(Depricated)"
	}
	return proxyType
}

func (c *DebugCommand) writeEnvoyProxyPodList(pods []v1.Pod) error {
	type podDataType map[string]map[string]string
	type proxyPodsDataType map[string][]podDataType

	proxyPodsData := make(proxyPodsDataType)
	for _, pod := range pods {
		podData := make(podDataType)
		age := time.Since(pod.CreationTimestamp.Time).Round(time.Minute)
		var readyCount int
		for _, status := range pod.Status.ContainerStatuses {
			if status.Ready {
				readyCount++
			}
		}
		readyStatus := fmt.Sprintf("%d/%d", readyCount, len(pod.Spec.Containers))

		// restartCount - shows how many times the container(s) within each pod have restarted.
		var totalRestartCount int32
		for _, status := range pod.Status.ContainerStatuses {
			totalRestartCount += status.RestartCount
		}

		ip := pod.Status.PodIP

		data := map[string]string{
			"ready":     readyStatus,
			"status":    string(pod.Status.Phase),
			"restart":   strconv.Itoa(int(totalRestartCount)),
			"age":       age.String(),
			"namespace": pod.Namespace,
			"ip":        ip,
		}
		podData[pod.Name] = data
		podProxyType := c.getPodProxyType(pod)
		proxyPodsData[podProxyType] = append(proxyPodsData[podProxyType], podData)
	}
	proxyPodsListPath := filepath.Join(c.output, "proxy", "proxyList.json")
	if err := os.MkdirAll(filepath.Dir(proxyPodsListPath), 0755); err != nil {
		return fmt.Errorf("error creating directory for proxy list json file: %w", err)
	}
	err := writeJSONFile(proxyPodsListPath, proxyPodsData)
	if err != nil {
		return fmt.Errorf("error writing proxy list to json file: %v", err)
	}
	return nil
}

// captureEnvoyProxyPodData - captures Envoy admin endpoint data (/stats, /clusters, /endpoints, /listeners, /config_dump) for a pod.
func (c *DebugCommand) captureEnvoyProxyPodData(pod v1.Pod, proxyType string, namespace string) error {

	pf := common.PortForward{
		Namespace:  namespace,
		PodName:    pod.Name,
		RemotePort: envoyDefaultAdminPort,
		KubeClient: c.kubernetes,
		RestConfig: c.restConfig,
	}

	var endpoint string
	var err error
	// Dependency injection for testing
	if c.envoyDefaultAdminPortEndpoint != "" {
		endpoint = c.envoyDefaultAdminPortEndpoint
	}
	if endpoint == "" {
		endpoint, err = pf.Open(c.Ctx)
		if err != nil {
			return fmt.Errorf("error port forwarding %s", err)
		}
		defer pf.Close()
	}

	var errs error
	err = c.captureEnvoyStats(endpoint, pod, proxyType, namespace)
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("error capturing envoy stats: %v", err))
	}
	err = c.captureEnvoyConfig(pod, proxyType, namespace)
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("error capturing envoy config: %v", err))
	}
	return errs
}

// captureEnvoyStats - captures envoy stats for a given pod (by opening a portforwarder the Envoy admin API)
func (c *DebugCommand) captureEnvoyStats(endpoint string, pod v1.Pod, proxyType string, namespace string) error {

	resp, err := http.Get(fmt.Sprintf("http://%s/stats?format=json", endpoint))
	if err != nil {
		return fmt.Errorf("error hitting stats endpoint of envoy: %s", err)
	}
	stats, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading body of http response%s", err)
	}
	defer resp.Body.Close()

	// Create file path and directory for storing logs
	proxyPodEnvoyStatsPath := filepath.Join(c.output, "proxy", namespace, proxyType, pod.Name, "stats.json")
	if err := os.MkdirAll(filepath.Dir(proxyPodEnvoyStatsPath), 0755); err != nil {
		return fmt.Errorf("error creating directory for enviy stats file: %w", err)
	}

	var statsJson interface{}
	err = json.Unmarshal(stats, &statsJson)
	if err != nil {
		return fmt.Errorf("error unmarshalling JSON: %s", err)
	}
	marshaled, err := json.MarshalIndent(statsJson, "", "\t")
	if err != nil {
		return fmt.Errorf("error marshalling JSON: %s", err)
	}

	if err := os.WriteFile(proxyPodEnvoyStatsPath, marshaled, 0644); err != nil {
		return fmt.Errorf("error writing envoy stats to json file for pod '%s': %v", pod.Name, err)
	}
	return nil
}

// captureEnvoyConfig
//   - captures the configuration from the config dump endpoint (by opening a port forwarder to the Envoy admin API).
//   - captures the raw config dumps (json) that are currently loaded configuration including EDS.
func (c *DebugCommand) captureEnvoyConfig(pod v1.Pod, proxyType string, namespace string) error {
	adminPorts, err := c.fetchAdminPorts(pod.Name, namespace)
	if err != nil {
		return err
	}

	configs := make(map[string]*envoy.EnvoyConfig, 0)

	for name, adminPort := range adminPorts {
		pf := common.PortForward{
			Namespace:  namespace,
			PodName:    pod.Name,
			RemotePort: adminPort,
			KubeClient: c.kubernetes,
			RestConfig: c.restConfig,
		}
		// Dependency injection for testing
		if c.fetchEnvoyConfig == nil {
			c.fetchEnvoyConfig = envoy.FetchConfig
		}
		config, err := c.fetchEnvoyConfig(c.Ctx, &pf)
		if err != nil {
			return fmt.Errorf("error fetching envoy config: %v", err)
		}
		configs[name] = config
	}
	cfgs := make(map[string]interface{})
	for name, config := range configs {
		cfg := make(map[string]interface{})
		cfg["clusters"] = config.Clusters
		cfg["endpoints"] = config.Endpoints
		cfg["listeners"] = config.Listeners
		cfg["routes"] = config.Routes
		cfg["secrets"] = config.Secrets
		cfgs[name] = cfg
	}
	configJson, err := json.MarshalIndent(cfgs, "", "\t")
	if err != nil {
		return fmt.Errorf("error marshalling the config json: %v", err)
	}

	configPath := filepath.Join(c.output, "proxy", namespace, proxyType, pod.Name, "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("error creating directory for envoy config file: %w", err)
	}
	if err := os.WriteFile(configPath, configJson, 0644); err != nil {
		return fmt.Errorf("error writing envoy configs to json file for pod '%s': %v", pod.Name, err)
	}

	// raw config_dumps
	config_dumps := make(map[string]interface{})
	for name, config := range configs {
		var config_dumps_json interface{}
		if err := json.Unmarshal(config.RawCfg, &config_dumps_json); err != nil {
			return fmt.Errorf("error unmarshalling the config dumps: %v", err)
		}
		config_dumps[name] = config_dumps_json
	}
	configDumpsJson, err := json.MarshalIndent(config_dumps, "", "\t")
	if err != nil {
		return fmt.Errorf("error marshalling the config dump json: %v", err)
	}

	rawConfigDumpsPath := filepath.Join(c.output, "proxy", namespace, proxyType, pod.Name, "config_dumps.json")
	if err := os.MkdirAll(filepath.Dir(rawConfigDumpsPath), 0755); err != nil {
		return fmt.Errorf("error creating directory for envoy config_dumps file: %w", err)
	}
	if err := os.WriteFile(rawConfigDumpsPath, configDumpsJson, 0644); err != nil {
		return fmt.Errorf("error writing envoy configs_dumps to json file for pod '%s': %v", pod.Name, err)
	}

	return nil
}

func (c *DebugCommand) fetchAdminPorts(podName string, namespace string) (map[string]int, error) {
	adminPorts := make(map[string]int, 0)
	pod, err := c.kubernetes.CoreV1().Pods(namespace).Get(c.Ctx, podName, metav1.GetOptions{})
	if err != nil {
		return adminPorts, err
	}

	connectService, isMultiport := pod.Annotations["consul.hashicorp.com/connect-service"]

	if !isMultiport {
		// Return the default port configuration.
		adminPorts[podName] = envoyDefaultAdminPort
		return adminPorts, nil
	}

	for index, service := range strings.Split(connectService, ",") {
		adminPorts[service] = envoyDefaultAdminPort + index
	}

	return adminPorts, nil
}
