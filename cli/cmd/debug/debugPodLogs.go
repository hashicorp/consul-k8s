// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package debug

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-multierror"
)

type logCollectionResult struct {
	StatusLine string // for audit file
	Err        error  // original error if any
}

type consulK8sComponents struct {
	clientList     *appsv1.DaemonSetList
	serverList     *appsv1.StatefulSetList
	deploymentList *appsv1.DeploymentList
}

type workload struct {
	Name     string          `json:"name"`     // consul-server
	Kind     string          `json:"kind"`     // statefulsets
	PodsList *corev1.PodList `json:"podsList"` // [consul-server-0, consul-server-1, ...]
}

func (c *DebugCommand) getConsulK8sComponents(ctx context.Context) (consulK8sComponents, error) {
	namespace := c.flagNamespace
	var errs error
	clients, err := c.kubernetes.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		err = multierror.Append(errs, fmt.Errorf("Unable to list consul-k8s clients, %s", err))
	}
	servers, err := c.kubernetes.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		err = multierror.Append(errs, fmt.Errorf("Unable to list consul-k8s servers, %s", err))
	}
	deployments, err := c.kubernetes.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		err = multierror.Append(errs, fmt.Errorf("Unable to list consul-k8s deployments, %s", err))
	}
	components := consulK8sComponents{
		clientList:     clients,
		serverList:     servers,
		deploymentList: deployments,
	}
	return components, errs
}
func (c *DebugCommand) getPodsForWorkload(ctx context.Context, namespace string, selector *metav1.LabelSelector) (*corev1.PodList, error) {
	var specSelectorString []string
	for key, value := range selector.MatchLabels {
		specSelectorString = append(specSelectorString, fmt.Sprintf("%s=%s", key, value))
	}
	labelSelector := strings.Join(specSelectorString, ",")

	return c.kubernetes.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}
func (c *DebugCommand) getComponentsWorkload(ctx context.Context, components consulK8sComponents) ([]workload, error) {
	var errs error
	workloads := []workload{}
	// statefulsets
	for _, server := range components.serverList.Items {
		podsList, err := c.getPodsForWorkload(ctx, server.Namespace, server.Spec.Selector)
		if err != nil {
			err = multierror.Append(errs, fmt.Errorf("Unable to list pods for Consul Server- '%s': %v\n", server.Name, err))
		}
		workloads = append(workloads, workload{server.Name, "statefulsets", podsList})
	}
	// daemonset
	for _, client := range components.clientList.Items {
		podsList, err := c.getPodsForWorkload(ctx, client.Namespace, client.Spec.Selector)
		if err != nil {
			err = multierror.Append(errs, fmt.Errorf("Unable to list pods for Consul Clients- '%s': %v\n", client.Name, err))
		}
		workloads = append(workloads, workload{client.Name, "daemonsets", podsList})
	}
	// deployments
	for _, deployment := range components.deploymentList.Items {
		podsList, err := c.getPodsForWorkload(ctx, deployment.Namespace, deployment.Spec.Selector)
		if err != nil {
			err = multierror.Append(errs, fmt.Errorf("Unable to list pods for Consul deployments- '%s': %v\n", deployment.Name, err))
		}
		workloads = append(workloads, workload{deployment.Name, "deployments", podsList})
	}
	// sidecars
	proxyPodList, err := c.kubernetes.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "consul.hashicorp.com/connect-inject-status=injected",
	})
	if err != nil {
		err = multierror.Append(errs, fmt.Errorf("Unable to list pods for consul-injected-proxy: %v\n", err))
	}
	workloads = append(workloads, workload{"consul-injected-envoy-proxy", "sidecarProxies", proxyPodList})
	return workloads, errs
}

// capturePodLogsAndEvents
// - retrieves consul-k8s components (server, client, injector, sidecar) pods
// - and fetches log & events for each of the pods
// - and write it to /pod dir within debug archive
func (c *DebugCommand) capturePodLogsAndEvents() error {
	c.UI.Output("\nCapturing pods info.....")
	components, err := c.getConsulK8sComponents(c.Ctx)
	if err != nil {
		c.UI.Output("%s", err, terminal.WithWarningStyle())
	}
	workloads, err := c.getComponentsWorkload(c.Ctx, components)
	if err != nil {
		c.UI.Output("%s", err, terminal.WithWarningStyle())
	}
	if len(workloads) == 0 {
		c.UI.Output("No Consul Component Found! \n")
		return nil
	}

	totalPods, totalContainers := 0, 0
	for _, workload := range workloads {
		for _, pod := range workload.PodsList.Items {
			totalPods++
			totalContainers += len(pod.Spec.Containers)
		}
	}
	// Output metadata about workload
	c.UI.Output(fmt.Sprintf(" - Total Pods:        %d", totalPods))
	c.UI.Output(fmt.Sprintf(" - Total Containers:  %d", totalContainers))

	err = c.getPodEvents(c.Ctx, workloads)
	if err != nil {
		c.UI.Output("%s", err, terminal.WithWarningStyle())
	}
	c.UI.Output("Pods Events Captured", terminal.WithSuccessStyle())

	c.UI.Output("\nCapturing pods logs.....")
	if c.since != 0 {
		c.UI.Output(fmt.Sprintf(" - Since:            %s", c.since))
		sinceSeconds := int64(c.since.Seconds())
		err = c.getPodLogs(c.Ctx, workloads, totalContainers, sinceSeconds)
	} else {
		c.UI.Output(fmt.Sprintf(" - Duration:         %s", c.duration))
		durationChn := time.After(c.duration)
		sinceSeconds := int64(c.duration.Seconds())
		select {
		case <-durationChn:
			err = c.getPodLogs(c.Ctx, workloads, totalContainers, sinceSeconds)
		case <-c.Ctx.Done():
			return signalInterruptError
		}
	}
	if err != nil {
		return err
	}
	c.UI.Output("Pods Logs captured", terminal.WithSuccessStyle())
	return nil
}

// =======================================================

func (c *DebugCommand) getPodEvents(ctx context.Context, workloads []workload) error {
	var errs error

	for _, workload := range workloads {
		if len(workload.PodsList.Items) > 0 {
			for _, pod := range workload.PodsList.Items {
				fieldSelector := fmt.Sprintf("involvedObject.name=%s", pod.Name)
				events, err := c.kubernetes.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{
					FieldSelector: fieldSelector,
				})
				if err != nil {
					errs = multierror.Append(errs, fmt.Errorf("Unable to capture Events for: Consul> '%s'(%s) -Pod %s : %v", workload.Name, workload.Kind, pod.Name, err))
				}

				// Create file path and directory for storing events
				eventFilePathText := filepath.Join(c.output, "pod", "events", workload.Kind, workload.Name, fmt.Sprintf("%s.txt", pod.Name))
				if err := os.MkdirAll(filepath.Dir(eventFilePathText), 0755); err != nil {
					errs = multierror.Append(errs, fmt.Errorf("Unable to create log directory for: Consul> '%s'(%s) -Pod %s : %v", workload.Name, workload.Kind, pod.Name, err))
				}
				formattedEvents := formatEventsLikeKubectlDescribe(events)
				if err := os.WriteFile(eventFilePathText, []byte(formattedEvents), 0644); err != nil {
					errs = multierror.Append(errs, fmt.Errorf("Unable to write event's text file for: Consul> '%s'(%s) -Pod %s : %v", workload.Name, workload.Kind, pod.Name, err))
				}
			}
		}
	}

	return errs
}
func formatEventsLikeKubectlDescribe(events *corev1.EventList) string {
	if len(events.Items) == 0 {
		return fmt.Sprintf("Events:\n  <none>\n")
	}

	var sb strings.Builder
	sb.WriteString("Events:\n")

	// Sort by timestamp (oldest first for kubectl format)
	sortedEvents := events.Items
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].LastTimestamp.Time.Before(sortedEvents[j].LastTimestamp.Time)
	})

	for _, event := range sortedEvents {
		age := time.Since(event.LastTimestamp.Time).Round(time.Second)
		ageStr := formatDuration(age)

		// kubectl format: Type|Reason|Age|From|Message
		sb.WriteString(fmt.Sprintf("  %-8s  %-20s  %-8s  %-15s  %s\n",
			event.Type,
			event.Reason,
			ageStr,
			event.Source.Component,
			truncateMessage(event.Message, 100)))
	}

	return sb.String()
}
func truncateMessage(message string, maxLength int) string {
	if len(message) <= maxLength {
		return message
	}
	return message[:maxLength] + "..."
}
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	default:
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
}

// =======================================================

// getPodLogs - fetches logs 'of each containers' 'of each pods' 'of each workload items' using k8s api with filter of 'sinceSeconds' and writes to log directory within debug archive.
func (c *DebugCommand) getPodLogs(ctx context.Context, workloads []workload, totalContainers int, sinceSeconds int64) error {

	// create logCaptureAudit file for each container logs collection
	auditFilePath := filepath.Join(c.output, "pod", "logs", "logCaptureAudit.txt")
	if err := os.MkdirAll(filepath.Dir(auditFilePath), 0755); err != nil {
		return fmt.Errorf("error creating logCaptureAudit directory: %v", err)
	}
	auditFile, err := os.OpenFile(auditFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error creating logCaptureAudit file: %v", err)
	}
	w := tabwriter.NewWriter(auditFile, 1, 3, 2, ' ', 0)
	fmt.Fprintln(w, "WORKLOAD-KIND\tWORKLOAD-NAME\tPOD-NAME\tCONTAINER-NAME\tLOG COLLECTION STATUS\tDETAILS")
	defer auditFile.Close()
	defer w.Flush()

	resultsChan := make(chan logCollectionResult, totalContainers)
	var wg sync.WaitGroup

	c.logCollector(ctx, &wg, workloads, resultsChan, sinceSeconds)
	go func() {
		wg.Wait()
		close(resultsChan)
	}()
	return c.resultCollectorAndAuditor(ctx, w, resultsChan)
}

func (c *DebugCommand) logCollector(ctx context.Context, wg *sync.WaitGroup, workloads []workload, resultsChan chan<- logCollectionResult, sinceSeconds int64) {
	sem := make(chan struct{}, 10) // Buffered Channel Semaphore: limit to 10 concurrent goroutines

	for _, workload := range workloads {
		if len(workload.PodsList.Items) == 0 {
			resultsChan <- logCollectionResult{
				StatusLine: fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", workload.Kind, workload.Name, "", "", "No Pods Found", "No Pods Found"),
			}
			continue
		}
		for _, pod := range workload.PodsList.Items {
			for _, container := range pod.Spec.Containers {
				wg.Add(1)
				sem <- struct{}{} // aquire semaphore {blocks if full}

				workload, pod, container := workload, pod, container // local copy for goroutine

				go func() {
					defer wg.Done()
					defer func() { <-sem }() // release semaphore when done
					logErr := c.getContainerLogs(ctx, sinceSeconds, pod.Namespace, pod.Name, container.Name, workload.Kind, workload.Name)
					var statusLine string
					if logErr != nil {
						statusLine = fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", workload.Kind, workload.Name, pod.Name, container.Name, "Failed", logErr.Error())
						logErr = fmt.Errorf("%s -> %s -> %s -> %s\n\t=> %v", workload.Kind, workload.Name, pod.Name, container.Name, logErr)
					} else {
						statusLine = fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", workload.Kind, workload.Name, pod.Name, container.Name, "Successful", "")
					}
					resultsChan <- logCollectionResult{StatusLine: statusLine, Err: logErr}
				}()
			}
			for _, container := range pod.Spec.InitContainers {
				wg.Add(1)
				sem <- struct{}{} // aquire semaphore {blocks if full}

				workload, pod, container := workload, pod, container // local copy for goroutine

				go func() {
					defer wg.Done()
					defer func() { <-sem }() // release semaphore when done
					logErr := c.getContainerLogs(ctx, sinceSeconds, pod.Namespace, pod.Name, container.Name, workload.Kind, workload.Name)
					var statusLine string
					if logErr != nil {
						statusLine = fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", workload.Kind, workload.Name, pod.Name, container.Name, "Failed", logErr.Error())
						logErr = fmt.Errorf("%s -> %s -> %s -> %s\n\t=> %v", workload.Kind, workload.Name, pod.Name, container.Name, logErr)
					} else {
						statusLine = fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", workload.Kind, workload.Name, pod.Name, container.Name, "Successful", "")
					}
					resultsChan <- logCollectionResult{StatusLine: statusLine, Err: logErr}
				}()
			}
		}
	}
}
func (c *DebugCommand) resultCollectorAndAuditor(ctx context.Context, w *tabwriter.Writer, resultsChan <-chan logCollectionResult) error {
	var logCaptureErrors *multierror.Error
	var tabWriterMutex sync.Mutex
	var auditWriteErrOnce sync.Once // Use sync.Once to report the write error only once.

ReadLoop:
	for {
		select {
		case result, ok := <-resultsChan:
			if !ok {
				// Channel closed, all results processed
				break ReadLoop
			}
			if result.Err != nil {
				logCaptureErrors = multierror.Append(logCaptureErrors, result.Err)
			}

			// Write the status line to the audit file.
			tabWriterMutex.Lock()
			_, writeErr := fmt.Fprintln(w, result.StatusLine)
			tabWriterMutex.Unlock()
			if writeErr != nil {
				// prevent flooding of write errors on terminal
				auditWriteErrOnce.Do(func() {
					c.UI.Output(
						fmt.Sprintf("error writing to audit file, it may be incomplete. First error: %v", writeErr),
						terminal.WithWarningStyle(),
					)
				})
			}
		case <-ctx.Done():
			logCaptureErrors = multierror.Append(logCaptureErrors, ctx.Err())
			break ReadLoop
		}
	}

	if logCaptureErrors.ErrorOrNil() != nil {
		errorFilePath := filepath.Join(c.output, "pod", "logs", "logCaptureErrors.txt")
		errorContent := []byte(logCaptureErrors.Error())
		if err := os.WriteFile(errorFilePath, errorContent, 0644); err != nil {
			return fmt.Errorf("error writing log capture errors to file: %v\n Collected Errors:\n%v", err, errorContent)
		}
		return fmt.Errorf("one or more errors occurred during log collection; \n\tplease check logs/logCaptureErrors.txt in debug archive for details")
	}
	return nil
}

// getContainerLogs - fetches logs for a container and write it to log file.
func (c *DebugCommand) getContainerLogs(ctx context.Context, sinceSeconds int64, namespace, podName, containerName, workloadKind, workloadName string) error {
	podLogOptions := &corev1.PodLogOptions{
		Container:    containerName,
		SinceSeconds: &sinceSeconds,
		Follow:       false,
		Timestamps:   true,
	}

	logFilePath := filepath.Join(c.output, "pod", "logs", workloadKind, workloadName, podName, fmt.Sprintf("%s.log", containerName))
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0755); err != nil {
		return fmt.Errorf("error creating log directory: %w", err)
	}
	logFile, err := os.Create(logFilePath)
	if err != nil {
		return fmt.Errorf("error creating log file: %w", err)
	}
	defer logFile.Close()

	podLogRequest := c.kubernetes.CoreV1().Pods(namespace).GetLogs(podName, podLogOptions)
	podLogStream, err := podLogRequest.Stream(ctx)
	if err != nil {
		return fmt.Errorf("error getting logs: %v", err)
	}
	defer podLogStream.Close()

	_, err = io.Copy(logFile, podLogStream)
	if err != nil {
		return fmt.Errorf("error copying log stream to file: %w", err)
	}

	// logs, err := io.ReadAll(podLogStream)
	// if err != nil {
	// 	return fmt.Errorf("error reading log streams: %w", err)
	// }
	// if err := os.WriteFile(logFilePath, logs, 0644); err != nil {
	// 	return fmt.Errorf("error writing log to file: %v", err)
	// }

	return nil
}
