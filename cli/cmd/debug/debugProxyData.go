// https://www.envoyproxy.io/docs/envoy/latest/operations/admin

package debug

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const envoyDefaultAdminPort = 19000

type proxyPodData struct {
	name      string
	pod       v1.Pod
	namespace string
	proxyType string
}

type EnvoyProxyCapture struct {
	// Debug command objects
	kubernetes kubernetes.Interface
	restConfig *rest.Config
	output     string
	ctx        context.Context

	// Internal state
	proxyPods []proxyPodData

	// Dependency injection for testing
	fetchEnvoyConfig              func(context.Context, common.PortForwarder) (*envoy.EnvoyConfig, error)
	envoyDefaultAdminPortEndpoint string
}

// captureEnvoyProxyData -
// captures consul-k8s Envoy admin endpoint data (/stats, /clusters, /listeners, /config_dump)
// for ALL proxy pods in ALL namespaces and writes it to /proxy dir within debug bundle
func (e *EnvoyProxyCapture) captureEnvoyProxyData() error {
	// get all proxy pods
	err := e.getEnvoyProxyPodsList()
	if err != nil {
		if err == notFoundError {
			return err
		}
		return fmt.Errorf("error fetching pods list: %s", err)
	}
	// write envoy proxy pods list to json file within debug bundle
	err = e.writeEnvoyProxyPodList()
	if err != nil {
		return err
	}

	// capture all proxy's details and write them to debug bundle
	var errs *multierror.Error
	for _, proxyPod := range e.proxyPods {
		if err := e.captureEnvoyProxyPodData(proxyPod); err != nil {
			err = fmt.Errorf("%s: %v\n", proxyPod.name, err)
			errs = multierror.Append(errs, err)
		}
	}
	// If any errors were collected during the capture, write them to a file in the debug directory.
	if errs.ErrorOrNil() != nil {
		errorFilePath := filepath.Join(e.output, "proxy", "proxyCaptureErrors.txt")
		errorContent := []byte(errs.Error())
		err := fileWriter(errorFilePath, errorContent)
		if err != nil {
			return fmt.Errorf("error writing proxy data capture errors to file: %v\n Collected Errors:\n%v", err, errorContent)
		}
		return oneOrMoreErrorOccured
	}
	return nil
}

// getEnvoyProxyPodsList - captures all pods in ALL Namespaces which run envoy proxies,
// making sure to return each pod only once even if multiple label selectors may return the same pod.
func (e *EnvoyProxyCapture) getEnvoyProxyPodsList() error {
	uniquePods := make(map[types.NamespacedName]proxyPodData)
	proxySelectors := []string{
		"component=api-gateway, gateway.consul.hashicorp.com/managed=true",
		"api-gateway.consul.hashicorp.com/managed=true", // Legacy api gateway
		"component=ingress-gateway, chart=consul-helm",
		"component=mesh-gateway, chart=consul-helm",
		"component=terminating-gateway, chart=consul-helm",
		"consul.hashicorp.com/connect-inject-status=injected",
	}
	for _, selector := range proxySelectors {
		pods, err := e.kubernetes.CoreV1().Pods("").List(e.ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			return err
		}
		// Add pods to the map, which handles uniqueness automatically
		for _, pod := range pods.Items {
			name := types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}
			uniquePods[name] = proxyPodData{
				namespace: pod.Namespace,
				pod:       pod,
				proxyType: e.getPodProxyType(pod),
				name:      pod.Name,
			}
		}
	}
	if len(uniquePods) == 0 {
		return notFoundError
	}
	// Convert the map values back into a slice
	var allPods []proxyPodData
	for _, pod := range uniquePods {
		allPods = append(allPods, pod)
	}
	e.proxyPods = allPods
	return nil
}

// getProxyType - takes k8s pod object returns its proxy type.
func (e *EnvoyProxyCapture) getPodProxyType(proxyPod v1.Pod) string {
	componentTypeMap := map[string]string{
		"api-gateway":         "API Gateway",
		"ingress-gateway":     "Ingress Gateway",
		"mesh-gateway":        "Mesh Gateway",
		"terminating-gateway": "Terminating Gateway",
	}
	proxyType := "Sidecar"

	if mappedType, ok := componentTypeMap[proxyPod.Labels["component"]]; ok {
		proxyType = mappedType
	} else if proxyPod.Labels["api-gateway.consul.hashicorp.com/managed"] == "true" {
		// Special case for deprecated API Gateway.
		proxyType = "API Gateway(Depricated)"
	}
	return proxyType
}

func (e *EnvoyProxyCapture) writeEnvoyProxyPodList() error {
	type podDataType map[string]map[string]string
	type proxyPodsDataType map[string][]podDataType

	proxyPodsData := make(proxyPodsDataType)
	for _, pp := range e.proxyPods {
		podData := make(podDataType)
		age := time.Since(pp.pod.CreationTimestamp.Time).Round(time.Minute)
		var readyCount int
		for _, status := range pp.pod.Status.ContainerStatuses {
			if status.Ready {
				readyCount++
			}
		}
		readyStatus := fmt.Sprintf("%d/%d", readyCount, len(pp.pod.Spec.Containers))

		// restartCount - shows how many times the container(s) within each pod have restarted.
		var totalRestartCount int32
		for _, status := range pp.pod.Status.ContainerStatuses {
			totalRestartCount += status.RestartCount
		}

		ip := pp.pod.Status.PodIP

		data := map[string]string{
			"ready":     readyStatus,
			"status":    string(pp.pod.Status.Phase),
			"restart":   strconv.Itoa(int(totalRestartCount)),
			"age":       age.String(),
			"namespace": pp.namespace,
			"ip":        ip,
		}
		podData[pp.name] = data
		proxyPodsData[pp.proxyType] = append(proxyPodsData[pp.proxyType], podData)
	}
	proxyPodsListPath := filepath.Join(e.output, "proxy", "proxyList.json")
	err := writeJSONFile(proxyPodsListPath, proxyPodsData)
	if err != nil {
		return fmt.Errorf("error writing proxy list to json file: %v", err)
	}
	return nil
}

// captureEnvoyProxyPodData - captures Envoy admin endpoint data (/stats, /clusters, /endpoints, /listeners, /config_dump) for a pod.
func (e *EnvoyProxyCapture) captureEnvoyProxyPodData(proxyPod proxyPodData) error {

	pf := common.PortForward{
		Namespace:  proxyPod.namespace,
		PodName:    proxyPod.name,
		RemotePort: envoyDefaultAdminPort,
		KubeClient: e.kubernetes,
		RestConfig: e.restConfig,
	}

	var endpoint string
	var err error
	// Dependency injection for testing
	if e.envoyDefaultAdminPortEndpoint != "" {
		endpoint = e.envoyDefaultAdminPortEndpoint
	}
	if endpoint == "" {
		endpoint, err = pf.Open(e.ctx)
		if err != nil {
			return fmt.Errorf("error port forwarding %s", err)
		}
		defer pf.Close()
	}

	var errs error
	err = e.captureEnvoyStats(endpoint, proxyPod)
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("error capturing envoy stats: %v", err))
	}
	err = e.captureEnvoyConfig(proxyPod)
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("error capturing envoy config: %v", err))
	}
	return errs
}

// captureEnvoyStats - captures envoy stats for a given pod (by opening a portforwarder the Envoy admin API)
func (e *EnvoyProxyCapture) captureEnvoyStats(endpoint string, proxyPod proxyPodData) error {

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
	proxyPodEnvoyStatsPath := filepath.Join(e.output, "proxy", proxyPod.namespace, proxyPod.proxyType, proxyPod.name, "stats.json")

	var statsJson bytes.Buffer
	if err := json.Indent(&statsJson, stats, "", "\t"); err != nil {
		return fmt.Errorf("error indenting JSON proxy stats output: %w", err)
	}
	err = fileWriter(proxyPodEnvoyStatsPath, statsJson.Bytes())
	if err != nil {
		return fmt.Errorf("error writing envoy stats to json file for pod '%s': %v", proxyPod.name, err)
	}
	return nil
}

// captureEnvoyConfig
//   - captures the configuration from the config dump endpoint (by opening a port forwarder to the Envoy admin API).
//   - captures the raw config dumps (json) that are currently loaded configuration including EDS.
func (e *EnvoyProxyCapture) captureEnvoyConfig(proxyPod proxyPodData) error {
	adminPorts, err := e.fetchAdminPorts(proxyPod)
	if err != nil {
		return err
	}

	configs := make(map[string]*envoy.EnvoyConfig, 0)

	for name, adminPort := range adminPorts {
		pf := common.PortForward{
			Namespace:  proxyPod.namespace,
			PodName:    proxyPod.name,
			RemotePort: adminPort,
			KubeClient: e.kubernetes,
			RestConfig: e.restConfig,
		}
		// Dependency injection for testing
		if e.fetchEnvoyConfig == nil {
			e.fetchEnvoyConfig = envoy.FetchConfig
		}
		config, err := e.fetchEnvoyConfig(e.ctx, &pf)
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

	configPath := filepath.Join(e.output, "proxy", proxyPod.namespace, proxyPod.proxyType, proxyPod.name, "config.json")
	err = fileWriter(configPath, configJson)
	if err != nil {
		return fmt.Errorf("error writing envoy config to json file for pod '%s': %v", proxyPod.name, err)
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

	rawConfigDumpsPath := filepath.Join(e.output, "proxy", proxyPod.namespace, proxyPod.proxyType, proxyPod.name, "config_dumps.json")
	err = fileWriter(rawConfigDumpsPath, configDumpsJson)
	if err != nil {
		return fmt.Errorf("error writing envoy config dumps to json file for pod '%s': %v", proxyPod.name, err)
	}
	return nil
}

func (e *EnvoyProxyCapture) fetchAdminPorts(proxyPod proxyPodData) (map[string]int, error) {
	adminPorts := make(map[string]int, 0)
	p, err := e.kubernetes.CoreV1().Pods(proxyPod.namespace).Get(e.ctx, proxyPod.name, metav1.GetOptions{})
	if err != nil {
		return adminPorts, err
	}

	connectService, isMultiport := p.Annotations["consul.hashicorp.com/connect-service"]

	if !isMultiport {
		// Return the default port configuration.
		adminPorts[proxyPod.name] = envoyDefaultAdminPort
		return adminPorts, nil
	}

	for index, service := range strings.Split(connectService, ",") {
		adminPorts[service] = envoyDefaultAdminPort + index
	}

	return adminPorts, nil
}
