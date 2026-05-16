package debug

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/envoy"
)

const (
	envoyDefaultAdminPort      = 19000
	proxyCaptureErrorsFileName = "proxyCaptureErrors.txt"
)

type proxyPodData struct {
	// Unexported fields for internal use
	pod       v1.Pod `json:"-"` // Ignore the large pod object in JSON output
	proxyType string `json:"-"`

	// Exported fields for JSON output
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"`
	Status    string `json:"status"`
	Restart   string `json:"restart"`
	Age       string `json:"age"`
	IP        string `json:"ip"`
}
type proxyPods struct {
	ProxyPodType string         `json:"proxyPodType"`
	ProxyPodData []proxyPodData `json:"proxyPods"`
}

type EnvoyProxyCapture struct {
	// Debug command objects
	kubernetes kubernetes.Interface
	restConfig *rest.Config
	output     string
	ctx        context.Context

	// Internal state
	proxyPodsList []proxyPods

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
		if errors.Is(err, errNotFound) {
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
	for _, proxyGroup := range e.proxyPodsList {
		// Iterate through each individual pod within the group
		for _, podData := range proxyGroup.ProxyPodData {
			if err := e.captureEnvoyProxyPodData(podData); err != nil {
				err = fmt.Errorf("%s/%s: %v", podData.Namespace, podData.Name, err)
				errs = multierror.Append(errs, err)
			}
		}
	}

	// return if context is cancelled before writing errors to file
	if e.ctx.Err() == context.Canceled {
		return errSignalInterrupt
	}
	// If any errors were collected during the capture, write them to a file in the debug bundle.
	if errs.ErrorOrNil() != nil {
		errorFilePath := filepath.Join(e.output, "proxy", proxyCaptureErrorsFileName)
		errorContent := []byte(errs.Error())
		err := fileWriter(errorFilePath, errorContent)
		if err != nil {
			return fmt.Errorf("error writing proxy data capture errors to file: %v\n Collected Errors:\n%v", err, errorContent)
		}
		return errMultipleErrorsOccuredAndWritten
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
				Namespace: pod.Namespace,
				pod:       pod,
				proxyType: e.getPodProxyType(pod),
				Name:      pod.Name,
			}
		}
	}
	if len(uniquePods) == 0 {
		return fmt.Errorf("No envoy proxy pods found in the cluster: %w", errNotFound)
	}

	// Group pods by their proxyType
	groupedPods := make(map[string][]proxyPodData)
	for _, pod := range uniquePods {
		groupedPods[pod.proxyType] = append(groupedPods[pod.proxyType], pod)
	}

	// Convert the grouped map into the final slice structure
	var allProxyPods []proxyPods
	for proxyType, pods := range groupedPods {
		allProxyPods = append(allProxyPods, proxyPods{
			ProxyPodType: proxyType,
			ProxyPodData: pods,
		})
	}
	e.proxyPodsList = allProxyPods
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
		proxyType = "API Gateway(Deprecated)"
	}
	return proxyType
}

func (e *EnvoyProxyCapture) writeEnvoyProxyPodList() error {
	// Iterate through the list of proxy groups (e.g., "Sidecar", "Ingress Gateway")
	// and populate the data for each pod.
	for i := range e.proxyPodsList {
		// Iterate through each pod within the group
		for j := range e.proxyPodsList[i].ProxyPodData {
			// Get a pointer to the pod data to modify it in place
			pd := &e.proxyPodsList[i].ProxyPodData[j]

			// Calculate Ready Status
			var readyCount int
			for _, status := range pd.pod.Status.ContainerStatuses {
				if status.Ready {
					readyCount++
				}
			}
			pd.Ready = fmt.Sprintf("%d/%d", readyCount, len(pd.pod.Spec.Containers))

			// Calculate Total Restarts
			var totalRestartCount int32
			for _, status := range pd.pod.Status.ContainerStatuses {
				totalRestartCount += status.RestartCount
			}
			pd.Restart = strconv.Itoa(int(totalRestartCount))

			// Set other fields from the pod object
			pd.Status = string(pd.pod.Status.Phase)
			pd.Age = time.Since(pd.pod.CreationTimestamp.Time).Round(time.Second).String()
			pd.IP = pd.pod.Status.PodIP
			pd.Name = pd.pod.Name
			pd.Namespace = pd.pod.Namespace
		}
	}

	// return if context is cancelled before writing files
	if e.ctx.Err() == context.Canceled {
		return errSignalInterrupt
	}

	proxyPodsListPath := filepath.Join(e.output, "proxy", "proxyList.json")
	// Marshal the entire, updated list of structs
	err := writeJSONFile(proxyPodsListPath, e.proxyPodsList)
	if err != nil {
		return fmt.Errorf("error writing proxy list to json file: %v", err)
	}
	return nil
}

// captureEnvoyProxyPodData - captures Envoy admin endpoint data (/stats, /clusters, /endpoints, /listeners, /config_dump) for a pod.
func (e *EnvoyProxyCapture) captureEnvoyProxyPodData(proxyPod proxyPodData) error {

	pf := common.PortForward{
		Namespace:  proxyPod.Namespace,
		PodName:    proxyPod.Name,
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
		var pfOpenErr error
		endpoint, pfOpenErr = pf.Open(e.ctx)
		if pfOpenErr != nil {
			return fmt.Errorf("error port forwarding %s", pfOpenErr)
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
	proxyPodEnvoyStatsPath := filepath.Join(e.output, "proxy", proxyPod.Namespace, proxyPod.proxyType, proxyPod.Name, "stats.json")

	var statsJson bytes.Buffer
	if err := json.Indent(&statsJson, stats, "", "\t"); err != nil {
		return fmt.Errorf("error indenting JSON proxy stats output: %w", err)
	}

	// return if context is cancelled before writing files
	if e.ctx.Err() == context.Canceled {
		return errSignalInterrupt
	}
	err = fileWriter(proxyPodEnvoyStatsPath, statsJson.Bytes())
	if err != nil {
		return fmt.Errorf("error writing envoy stats to json file for pod '%s': %v", proxyPod.Name, err)
	}
	return nil
}

// captureEnvoyConfig
//   - captures the configuration from the config dump endpoint (by opening a port forwarder to the Envoy admin API).
//   - captures the raw config dumps (in json format) that are currently loaded configuration in envoy including EDS.
func (e *EnvoyProxyCapture) captureEnvoyConfig(proxyPod proxyPodData) error {
	adminPorts, err := e.fetchAdminPorts(proxyPod)
	if err != nil {
		return err
	}

	configs := make(map[string]*envoy.EnvoyConfig, 0)

	for name, adminPort := range adminPorts {
		pf := common.PortForward{
			Namespace:  proxyPod.Namespace,
			PodName:    proxyPod.Name,
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

	// return if context is cancelled before writing files
	if e.ctx.Err() == context.Canceled {
		return errSignalInterrupt
	}
	configPath := filepath.Join(e.output, "proxy", proxyPod.Namespace, proxyPod.proxyType, proxyPod.Name, "config.json")
	err = fileWriter(configPath, configJson)
	if err != nil {
		return fmt.Errorf("error writing envoy config to json file for pod '%s': %v", proxyPod.Name, err)
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

	// return if context is cancelled before writing files
	if e.ctx.Err() == context.Canceled {
		return errSignalInterrupt
	}
	rawConfigDumpsPath := filepath.Join(e.output, "proxy", proxyPod.Namespace, proxyPod.proxyType, proxyPod.Name, "config_dumps.json")
	err = fileWriter(rawConfigDumpsPath, configDumpsJson)
	if err != nil {
		return fmt.Errorf("error writing envoy config dumps to json file for pod '%s': %v", proxyPod.Name, err)
	}
	return nil
}

func (e *EnvoyProxyCapture) fetchAdminPorts(proxyPod proxyPodData) (map[string]int, error) {
	adminPorts := make(map[string]int, 0)
	p, err := e.kubernetes.CoreV1().Pods(proxyPod.Namespace).Get(e.ctx, proxyPod.Name, metav1.GetOptions{})
	if err != nil {
		return adminPorts, err
	}

	connectService, isMultiport := p.Annotations["consul.hashicorp.com/connect-service"]

	if !isMultiport {
		// Return the default port configuration.
		adminPorts[proxyPod.Name] = envoyDefaultAdminPort
		return adminPorts, nil
	}

	for index, service := range strings.Split(connectService, ",") {
		adminPorts[service] = envoyDefaultAdminPort + index
	}

	return adminPorts, nil
}
