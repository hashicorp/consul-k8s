// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package debug

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"text/tabwriter"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/go-multierror"
)

const (
	logCaptureAuditFileHeader = "WORKLOAD-KIND\tWORKLOAD-NAME\tPOD-NAME\tCONTAINER-NAME\tSTATUS\tDETAILS"

	// file names
	logCaptureAuditFileName  = "logCaptureAudit.txt"
	logCaptureErrorsFileName = "logCaptureErrors.txt"
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
	name     string          // consul-server
	kind     string          // statefulsets
	podsList *corev1.PodList // [consul-server-0, consul-server-1, ...]
}

type containerData struct {
	pod           corev1.Pod
	podName       string
	container     corev1.Container
	containerName string
	workloadName  string
	workloadKind  string
	namespace     string
}

type LogCapture struct {
	*common.BaseCommand
	// Debug command objects
	kubernetes kubernetes.Interface
	namespace  string
	ctx        context.Context
	output     string
	since      time.Duration
	duration   time.Duration

	// Internal states
	components          consulK8sComponents
	workloads           []workload
	k8sSinceSecondParam int64

	// Channels for log collection

	// containerChan will be pushed with all containers of all pods of all workload items
	// and will be consumed by getContainerLogAndUpdateResult workers.
	containersChan chan containerData
	// resultsChan will be pushed with logCollectionResult of each container
	// and will be consumed by resultCollectorAndAuditor.
	resultsChan chan logCollectionResult

	// Workload Metadata
	totalContainers int
	totalPods       int

	// Dependency injection for testing
	fetchLogsFunc func(string, string, *corev1.PodLogOptions) (io.ReadCloser, error)
}

func (l *LogCapture) getConsulK8sComponents() error {
	var comp consulK8sComponents
	var errs error
	var err error
	comp.clientList, err = l.kubernetes.AppsV1().DaemonSets(l.namespace).List(l.ctx,
		metav1.ListOptions{LabelSelector: "app=consul,chart=consul-helm,component=client"})
	if err != nil {
		err = multierror.Append(errs, fmt.Errorf("Unable to list consul-k8s clients, %s", err))
	}
	comp.serverList, err = l.kubernetes.AppsV1().StatefulSets(l.namespace).List(l.ctx,
		metav1.ListOptions{LabelSelector: "app=consul,chart=consul-helm,component=server"})
	if err != nil {
		err = multierror.Append(errs, fmt.Errorf("Unable to list consul-k8s servers, %s", err))
	}
	comp.deploymentList, err = l.kubernetes.AppsV1().Deployments(l.namespace).List(l.ctx, metav1.ListOptions{})
	if err != nil {
		err = multierror.Append(errs, fmt.Errorf("Unable to list consul-k8s deployments, %s", err))
	}
	l.components = comp
	return errs
}
func (l *LogCapture) getPodsForWorkload(selector *metav1.LabelSelector) (*corev1.PodList, error) {
	labelSelector := labels.SelectorFromSet(selector.MatchLabels).String()
	return l.kubernetes.CoreV1().Pods(l.namespace).List(l.ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}
func (l *LogCapture) getComponentsWorkload() error {
	var errs error
	workloads := []workload{}

	// statefulsets
	for _, server := range l.components.serverList.Items {
		podsList, err := l.getPodsForWorkload(server.Spec.Selector)
		if err != nil {
			err = multierror.Append(errs, fmt.Errorf("Unable to list pods for Consul Server- '%s': %v\n", server.Name, err))
		}
		workloads = append(workloads, workload{server.Name, "statefulsets", podsList})
	}
	// daemonset
	for _, client := range l.components.clientList.Items {
		podsList, err := l.getPodsForWorkload(client.Spec.Selector)
		if err != nil {
			err = multierror.Append(errs, fmt.Errorf("Unable to list pods for Consul Clients- '%s': %v\n", client.Name, err))
		}
		workloads = append(workloads, workload{client.Name, "daemonsets", podsList})
	}
	// deployments
	for _, deployment := range l.components.deploymentList.Items {
		podsList, err := l.getPodsForWorkload(deployment.Spec.Selector)
		if err != nil {
			err = multierror.Append(errs, fmt.Errorf("Unable to list pods for Consul deployments- '%s': %v\n", deployment.Name, err))
		}
		workloads = append(workloads, workload{deployment.Name, "deployments", podsList})
	}
	// sidecars
	proxyPodList, err := l.kubernetes.CoreV1().Pods("").List(l.ctx, metav1.ListOptions{
		LabelSelector: "consul.hashicorp.com/connect-inject-status=injected",
	})
	if err != nil {
		err = multierror.Append(errs, fmt.Errorf("Unable to list pods for consul-injected-proxy: %v\n", err))
	}
	workloads = append(workloads, workload{"sidecar", "sidecars", proxyPodList})

	l.workloads = workloads
	return errs
}

// pushWorkloadContainers - pushes all containers of all pods of all workload items to containersChan
func (l *LogCapture) pushWorkloadContainers() {
	for _, workload := range l.workloads {
		if len(workload.podsList.Items) == 0 {
			l.resultsChan <- logCollectionResult{
				StatusLine: fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", workload.kind, workload.name, "", "", "No Pods Found", "No Pods Found"),
			}
			continue
		}
		for _, pod := range workload.podsList.Items {
			containerData := containerData{
				pod:          pod,
				podName:      pod.Name,
				workloadName: workload.name,
				workloadKind: workload.kind,
				namespace:    pod.Namespace,
			}
			for _, container := range pod.Spec.Containers {
				containerData.container = container
				containerData.containerName = container.Name
				l.containersChan <- containerData
			}
			for _, container := range pod.Spec.InitContainers {
				containerData.container = container
				containerData.containerName = container.Name
				l.containersChan <- containerData
			}
		}
	}
}

// captureLogs
// - retrieves consul-k8s components (server, client, injector, sidecar) pods
// - and fetches log for each of the pods and write it to /pod dir within debug archive
// - also, writes log capture status to logCaptureAudit file and errors to logCaptureErrors file.
func (l *LogCapture) captureLogs() error {
	l.UI.Output("\nCapturing pods logs.....")
	err := l.getConsulK8sComponents()
	if err != nil {
		l.UI.Output("%s", err, terminal.WithWarningStyle())
	}
	err = l.getComponentsWorkload()
	if err != nil {
		l.UI.Output("%s", err, terminal.WithWarningStyle())
	}
	if len(l.workloads) == 0 {
		l.UI.Output("No Consul Component Found! \n")
		return nil
	}

	l.totalPods, l.totalContainers = 0, 0
	for _, workload := range l.workloads {
		for _, pod := range workload.podsList.Items {
			l.totalPods++
			l.totalContainers += len(pod.Spec.Containers) + len(pod.Spec.InitContainers)
		}
	}
	if l.totalPods == 0 {
		return errNotFound
	}

	// Output metadata about workload
	l.outputLogCaptureMetadata()

	if l.since != 0 {
		l.since += debugGraceDuration
		l.k8sSinceSecondParam = int64(l.since.Seconds())
		err = l.getWorkloadLogs()
	} else {
		l.duration += debugGraceDuration
		l.k8sSinceSecondParam = int64(l.duration.Seconds())
		durationChn := time.After(l.duration)
		select {
		case <-durationChn:
			err = l.getWorkloadLogs()
		case <-l.ctx.Done():
			return errSignalInterrupt
		}
	}
	if err != nil {
		return err
	}
	return nil
}

// getWorkloadLogs - fetches logs 'of each containers' 'of each pods' 'of each workload items'.
// write log status to logCaptureAudit file and errors to logCaptureErrors file.
func (l *LogCapture) getWorkloadLogs() error {

	// create logCaptureAudit file for each container logs collection
	logCaptureAuditFilePath := filepath.Join(l.output, "logs", logCaptureAuditFileName)
	if err := os.MkdirAll(filepath.Dir(logCaptureAuditFilePath), dirPerm); err != nil {
		return fmt.Errorf("error creating logCaptureAudit directory: %v", err)
	}
	auditFile, err := os.OpenFile(logCaptureAuditFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("error creating logCaptureAudit file: %v", err)
	}
	w := tabwriter.NewWriter(auditFile, 1, 3, 2, ' ', 0)
	fmt.Fprintln(w, logCaptureAuditFileHeader)
	defer auditFile.Close()
	defer w.Flush()

	// initialize channels
	l.resultsChan = make(chan logCollectionResult, l.totalContainers)
	l.containersChan = make(chan containerData, l.totalContainers)

	// initialize worker pool of log collector - getContainerLogAndUpdateResult
	var wg sync.WaitGroup
	numWorkers := int(math.Min(10, float64(l.totalContainers)))
	for i := 0; i < numWorkers; i++ {
		wg.Go(func() {
			l.getContainerLogAndUpdateResult()
		})
	}

	// fetch all containers of the workload and push it to containersChan
	l.pushWorkloadContainers()
	close(l.containersChan)

	// seperate goroutine to close resultsChan
	// as soon as all logCollector workers are done.
	go func() {
		wg.Wait()
		close(l.resultsChan)
	}()

	// resultCollectorAndAuditor-
	// read from resultsChan and write to audit file and error file.
	// Please Note: this function is blocking and will return only when
	// resultsChan is closed and all results are read from it or SIGINT is received.
	return l.resultCollectorAndAuditor(w)
}

// resultCollectorAndAuditor - collects results & errors of each resource (from logCollector) and writes to audit & error file resp.
func (l *LogCapture) resultCollectorAndAuditor(w *tabwriter.Writer) error {
	var logCaptureErrors *multierror.Error
	var tabWriterMutex sync.Mutex
	var auditWriteErrOnce sync.Once

	for {
		select {
		case <-l.ctx.Done():
			return errSignalInterrupt
		case result, ok := <-l.resultsChan:
			if !ok {
				if logCaptureErrors.ErrorOrNil() != nil {
					logCaptureErrorsFilePath := filepath.Join(l.output, "logs", logCaptureErrorsFileName)
					errorContent := []byte(logCaptureErrors.Error())
					if err := os.WriteFile(logCaptureErrorsFilePath, errorContent, filePerm); err != nil {
						return fmt.Errorf("error writing log capture errors to file: %v\n Collected Errors:\n%v", err, errorContent)
					}
					return errMultipleErrorsOccuredAndWritten
				}
				return nil
			}

			if l.ctx.Err() != nil {
				return errSignalInterrupt
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
					l.UI.Output(
						fmt.Sprintf("error writing results to audit file, it may be incomplete, error: %v", writeErr),
						terminal.WithWarningStyle(),
					)
				})
			}
		}
	}
}

// getContainerLogAndUpdateResult - is a worker to fetch logs for a container from containersChan and write its status to resultsChan
func (l *LogCapture) getContainerLogAndUpdateResult() {
	for cd := range l.containersChan {
		logErr := l.getContainerLogs(cd)

		// write log status to results channel
		var statusLine string
		if logErr != nil {
			statusLine = fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", cd.workloadKind, cd.workloadName, cd.podName, cd.containerName, "Failed", logErr.Error())
			logErr = fmt.Errorf("%s -> %s -> %s -> %s\n\t=> %v", cd.workloadKind, cd.workloadName, cd.podName, cd.containerName, logErr)
		} else {
			statusLine = fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", cd.workloadKind, cd.workloadName, cd.podName, cd.containerName, "Successful", "")
		}
		l.resultsChan <- logCollectionResult{StatusLine: statusLine, Err: logErr}
	}
}

// getContainerLogs - fetches logs for a container and write it to log file.
func (l *LogCapture) getContainerLogs(cd containerData) error {
	podLogOptions := &corev1.PodLogOptions{
		Container:    cd.containerName,
		SinceSeconds: &l.k8sSinceSecondParam,
		Follow:       false,
		Timestamps:   true,
	}

	logFilePath := filepath.Join(l.output, "logs", cd.workloadKind, cd.workloadName, cd.podName, fmt.Sprintf("%s.log", cd.containerName))
	if err := os.MkdirAll(filepath.Dir(logFilePath), dirPerm); err != nil {
		return fmt.Errorf("error creating log directory: %w", err)
	}
	logFile, err := os.Create(logFilePath)
	if err != nil {
		return fmt.Errorf("error creating log file: %w", err)
	}
	defer logFile.Close()

	// Dependency Injection for easier testing
	if l.fetchLogsFunc == nil {
		l.fetchLogsFunc = l.fetchLogs

	}
	podLogStream, err := l.fetchLogsFunc(cd.namespace, cd.podName, podLogOptions)
	if err != nil {
		return err
	}
	defer podLogStream.Close()

	_, err = io.Copy(logFile, podLogStream)
	if err != nil {
		return fmt.Errorf("error copying log stream to file: %w", err)
	}
	return nil
}

// fetchLogs - fetches the log stream for a given pod and container using the Kubernetes API.
func (l *LogCapture) fetchLogs(namespace, podName string, podLogOptions *corev1.PodLogOptions) (io.ReadCloser, error) {
	podLogRequest := l.kubernetes.CoreV1().Pods(namespace).GetLogs(podName, podLogOptions)
	podLogStream, err := podLogRequest.Stream(l.ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting log stream: %v", err)
	}
	return podLogStream, nil
}

func (l *LogCapture) outputLogCaptureMetadata() {
	l.UI.Output(fmt.Sprintf(" - Total Pods:        %d", l.totalPods))
	l.UI.Output(fmt.Sprintf(" - Total Containers:  %d", l.totalContainers))
	if l.since != 0 {
		l.UI.Output(fmt.Sprintf(" - Since:             %s", l.since))
	} else {
		l.UI.Output(fmt.Sprintf(" - Duration:          %s", l.duration))
	}
}
