// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package convertmultiportservices

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	mapset "github.com/deckarep/golang-set"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	cmdcommon "github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	cmdflags "github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

const (
	conversionStrategyTranslate    = "TRANSLATE"
	conversionStrategyDecommission = "DECOMMISSION"
)

// workloadKind identifies a Kubernetes workload that embeds a Pod template.
type workloadKind string

const (
	kindDeployment  workloadKind = "Deployment"
	kindStatefulSet workloadKind = "StatefulSet"
	kindDaemonSet   workloadKind = "DaemonSet"
)

// podListPageSize bounds the memory used by the stranded-workload scan on large
// clusters, where listing every Pod in a single response is expensive.
const podListPageSize = 500

// convertedWorkloadKinds is the ordered set of kinds this command rewrites.
//
// The connect-inject webhook admits Pods, not workloads, so every controller
// that owns a Pod template is subject to the multi-port registration gate. A
// kind that is missing here is not rejected at upgrade time; it keeps running
// until its Pods are recreated and only then starts failing admission, which
// strands StatefulSet ordinals and leaves new DaemonSet nodes without Pods.
var convertedWorkloadKinds = []workloadKind{kindDeployment, kindStatefulSet, kindDaemonSet}

// workloadRef identifies a single workload to convert.
type workloadRef struct {
	kind      workloadKind
	namespace string
	name      string
}

func (r workloadRef) String() string {
	return fmt.Sprintf("%s %s/%s", r.kind, r.namespace, r.name)
}

// workload is a fetched object reduced to what the conversion needs: the Pod
// template to rewrite and a way to persist the mutated parent object.
type workload struct {
	template *corev1.PodTemplateSpec
	update   func(context.Context) error
}

// Command applies an explicitly selected multi-port conversion policy to
// existing mesh-eligible workloads.
type Command struct {
	UI cli.Ui

	flagSet               *flag.FlagSet
	k8s                   *cmdflags.K8SFlags
	flagStrategy          string
	flagDefaultInject     bool
	flagReleaseNamespace  string
	flagAllowNamespaces   cmdflags.AppendSliceValue
	flagDenyNamespaces    cmdflags.AppendSliceValue
	flagNamespaceSelector string
	flagLogLevel          string
	flagLogJSON           bool

	once      sync.Once
	help      string
	k8sClient kubernetes.Interface
	logger    hclog.Logger
}

func (c *Command) init() {
	c.flagSet = flag.NewFlagSet("", flag.ContinueOnError)
	c.k8s = &cmdflags.K8SFlags{}
	c.flagSet.StringVar(&c.flagStrategy, "strategy", "", "Conversion strategy: TRANSLATE or DECOMMISSION.")
	c.flagSet.BoolVar(&c.flagDefaultInject, "default-inject", false, "Whether workloads without an explicit annotation are mesh-injectable.")
	c.flagSet.StringVar(&c.flagReleaseNamespace, "release-namespace", "", "The Consul Helm release namespace. Workloads in this namespace are never converted.")
	c.flagSet.Var(&c.flagAllowNamespaces, "allow-k8s-namespace", "Kubernetes namespace to allow. May be specified multiple times.")
	c.flagSet.Var(&c.flagDenyNamespaces, "deny-k8s-namespace", "Kubernetes namespace to deny. May be specified multiple times.")
	c.flagSet.StringVar(&c.flagNamespaceSelector, "namespace-selector", "", "JSON Kubernetes LabelSelector applied to namespaces.")
	c.flagSet.StringVar(&c.flagLogLevel, "log-level", "info", "Log verbosity level.")
	c.flagSet.BoolVar(&c.flagLogJSON, "log-json", false, "Enable JSON log output.")
	cmdflags.Merge(c.flagSet, c.k8s.Flags())
	c.help = cmdflags.Usage(help, c.flagSet)
}

func (c *Command) Run(args []string) int {
	c.once.Do(c.init)
	if err := c.flagSet.Parse(args); err != nil {
		return 1
	}
	if len(c.flagAllowNamespaces) == 0 {
		c.flagAllowNamespaces = cmdflags.AppendSliceValue{"*"}
	}

	c.flagStrategy = strings.ToUpper(strings.TrimSpace(c.flagStrategy))
	if c.flagStrategy != conversionStrategyTranslate && c.flagStrategy != conversionStrategyDecommission {
		c.UI.Error(fmt.Sprintf("unsupported conversion strategy %q; expected %s or %s", c.flagStrategy, conversionStrategyTranslate, conversionStrategyDecommission))
		return 1
	}

	if c.logger == nil {
		logger, err := cmdcommon.Logger(c.flagLogLevel, c.flagLogJSON)
		if err != nil {
			c.UI.Error(err.Error())
			return 1
		}
		c.logger = logger
	}

	if c.k8sClient == nil {
		config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}
		client, err := kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
		c.k8sClient = client
	}

	updated, err := c.convert(context.Background())
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error applying multi-port conversion: %s", err))
		return 1
	}
	c.logger.Info("multi-port conversion complete", "strategy", c.flagStrategy, "workloads_updated", updated)
	return 0
}

func (c *Command) convert(ctx context.Context) (int, error) {
	namespaces, err := c.eligibleNamespaces(ctx)
	if err != nil {
		return 0, err
	}

	refs, err := c.listWorkloads(ctx)
	if err != nil {
		return 0, err
	}

	updated := 0
	var errs []error
	for _, ref := range refs {
		if _, ok := namespaces[ref.namespace]; !ok {
			continue
		}
		changed, err := c.convertWorkload(ctx, ref)
		if err != nil {
			// Keep going so that a single malformed workload does not hide the
			// state of every workload after it. The command still fails, which
			// fails the Helm upgrade.
			c.logger.Error("failed to convert workload",
				"strategy", c.flagStrategy,
				"kind", string(ref.kind),
				"namespace", ref.namespace,
				"name", ref.name,
				"error", err,
			)
			errs = append(errs, err)
			continue
		}
		if changed {
			updated++
		}
	}

	stranded, err := c.strandedWorkloads(ctx, namespaces)
	if err != nil {
		errs = append(errs, err)
	} else if len(stranded) > 0 {
		errs = append(errs, strandedWorkloadError(stranded))
	}

	return updated, errors.Join(errs...)
}

// strandedWorkloads reports multi-port Pods whose owner this command cannot
// rewrite: bare Pods and controllers outside convertedWorkloadKinds, such as
// Argo Rollouts, OpenShift DeploymentConfigs, or operator-owned CRDs.
//
// Detection starts from Pods because the admission gate applies to Pods, so
// every affected workload is reachable this way regardless of its owner's kind.
// Rewriting is deliberately not attempted: patching arbitrary kinds would
// require near-cluster-wide write access, and an operator would revert the
// change on its next reconcile. Failing the upgrade with an explicit list turns
// a silent outage at the next Pod recreation into an actionable error now.
func (c *Command) strandedWorkloads(ctx context.Context, namespaces map[string]struct{}) ([]workloadRef, error) {
	seen := make(map[workloadRef]struct{})
	var stranded []workloadRef

	continueToken := ""
	for {
		pods, err := c.k8sClient.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
			Limit:    podListPageSize,
			Continue: continueToken,
		})
		if err != nil {
			return nil, fmt.Errorf("listing Pods: %w", err)
		}

		for _, pod := range pods.Items {
			if _, ok := namespaces[pod.Namespace]; !ok {
				continue
			}
			affected, err := podSelectsMultiplePorts(pod, c.flagDefaultInject)
			if err != nil {
				// A Pod with an unparseable annotation is already reported
				// against its owner by the mutation pass when that owner is a
				// converted kind, so only note it here.
				c.logger.Debug("skipping Pod with invalid annotations during stranded-workload scan",
					"namespace", pod.Namespace, "name", pod.Name, "error", err)
				continue
			}
			if !affected {
				continue
			}

			ref, ok, err := c.rootOwner(ctx, pod)
			if err != nil {
				return nil, err
			}
			if !ok || isConvertedKind(ref.kind) {
				// Already handled by the mutation pass. Its existing Pods still
				// carry the old annotations until the rollout completes, so they
				// are expected to match here.
				continue
			}
			if _, duplicate := seen[ref]; duplicate {
				continue
			}
			seen[ref] = struct{}{}
			stranded = append(stranded, ref)
		}

		continueToken = pods.Continue
		if continueToken == "" {
			break
		}
	}

	sort.Slice(stranded, func(i, j int) bool {
		return stranded[i].String() < stranded[j].String()
	})
	return stranded, nil
}

func strandedWorkloadError(stranded []workloadRef) error {
	names := make([]string, 0, len(stranded))
	for _, ref := range stranded {
		names = append(names, ref.String())
	}
	return fmt.Errorf(
		"%d workload(s) select multiple service ports but are not a %s, so this conversion cannot rewrite them: %s. "+
			"Set %s to a single port on each one, or remove them from the mesh, before disabling multi-port registration",
		len(stranded),
		strings.Join(convertedKindNames(), ", "),
		strings.Join(names, "; "),
		constants.AnnotationPort,
	)
}

// podSelectsMultiplePorts reports whether a Pod would be rejected by the
// connect-inject multi-port admission gate.
//
// Only the annotation is consulted. An injected Pod also carries the sidecar
// and init containers, whose ports would otherwise be counted as application
// ports; the webhook defaults this annotation before injecting, so an injected
// multi-port Pod always has it set.
func podSelectsMultiplePorts(pod corev1.Pod, defaultInject bool) (bool, error) {
	template := corev1.PodTemplateSpec{ObjectMeta: pod.ObjectMeta, Spec: pod.Spec}
	inject, err := shouldInject(template, defaultInject)
	if err != nil {
		return false, err
	}
	if !inject {
		return false, nil
	}
	// The legacy multi-service model registers one service per name and is not
	// subject to the gate.
	if hasMultipleConnectServices(pod.Annotations[constants.AnnotationService]) {
		return false, nil
	}
	return nonEmptyValueCount(pod.Annotations[constants.AnnotationPort]) > 1, nil
}

// rootOwner resolves the controller that owns a Pod's template. A ReplicaSet is
// followed one hop to its own controller, because rewriting a ReplicaSet is
// reverted by the Deployment that manages it.
func (c *Command) rootOwner(ctx context.Context, pod corev1.Pod) (workloadRef, bool, error) {
	owner := controllerRef(pod.OwnerReferences)
	if owner == nil {
		return workloadRef{kind: "Pod", namespace: pod.Namespace, name: pod.Name}, true, nil
	}

	if owner.Kind != "ReplicaSet" {
		return workloadRef{kind: workloadKind(owner.Kind), namespace: pod.Namespace, name: owner.Name}, true, nil
	}

	replicaSet, err := c.k8sClient.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// The ReplicaSet is gone, so this Pod is being torn down and its owner
		// cannot be identified or acted on.
		return workloadRef{}, false, nil
	}
	if err != nil {
		return workloadRef{}, false, fmt.Errorf("resolving owner of Pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}

	if replicaSetOwner := controllerRef(replicaSet.OwnerReferences); replicaSetOwner != nil {
		return workloadRef{kind: workloadKind(replicaSetOwner.Kind), namespace: pod.Namespace, name: replicaSetOwner.Name}, true, nil
	}
	return workloadRef{kind: "ReplicaSet", namespace: pod.Namespace, name: replicaSet.Name}, true, nil
}

func controllerRef(refs []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			return &refs[i]
		}
	}
	return nil
}

func isConvertedKind(kind workloadKind) bool {
	for _, converted := range convertedWorkloadKinds {
		if converted == kind {
			return true
		}
	}
	return false
}

func convertedKindNames() []string {
	names := make([]string, 0, len(convertedWorkloadKinds))
	for _, kind := range convertedWorkloadKinds {
		names = append(names, string(kind))
	}
	return names
}

// listWorkloads returns every convertible workload in the cluster, grouped by
// kind and sorted within each kind so the Job processes and logs workloads in a
// stable order across retries.
func (c *Command) listWorkloads(ctx context.Context) ([]workloadRef, error) {
	var refs []workloadRef
	for _, kind := range convertedWorkloadKinds {
		found, err := c.listWorkloadsOfKind(ctx, kind)
		if err != nil {
			return nil, err
		}
		sort.Slice(found, func(i, j int) bool {
			return found[i].namespace+"/"+found[i].name < found[j].namespace+"/"+found[j].name
		})
		refs = append(refs, found...)
	}
	return refs, nil
}

func (c *Command) listWorkloadsOfKind(ctx context.Context, kind workloadKind) ([]workloadRef, error) {
	var refs []workloadRef
	switch kind {
	case kindDeployment:
		items, err := c.k8sClient.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("listing Deployments: %w", err)
		}
		for _, item := range items.Items {
			refs = append(refs, workloadRef{kind: kind, namespace: item.Namespace, name: item.Name})
		}
	case kindStatefulSet:
		items, err := c.k8sClient.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("listing StatefulSets: %w", err)
		}
		for _, item := range items.Items {
			refs = append(refs, workloadRef{kind: kind, namespace: item.Namespace, name: item.Name})
		}
	case kindDaemonSet:
		items, err := c.k8sClient.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("listing DaemonSets: %w", err)
		}
		for _, item := range items.Items {
			refs = append(refs, workloadRef{kind: kind, namespace: item.Namespace, name: item.Name})
		}
	default:
		return nil, fmt.Errorf("unsupported workload kind %q", kind)
	}
	return refs, nil
}

// getWorkload fetches a workload and exposes its Pod template together with an
// update closure bound to the same object, so callers mutate and persist the
// object read in this attempt rather than a stale copy.
func (c *Command) getWorkload(ctx context.Context, ref workloadRef) (*workload, error) {
	switch ref.kind {
	case kindDeployment:
		object, err := c.k8sClient.AppsV1().Deployments(ref.namespace).Get(ctx, ref.name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return &workload{template: &object.Spec.Template, update: func(ctx context.Context) error {
			_, err := c.k8sClient.AppsV1().Deployments(ref.namespace).Update(ctx, object, metav1.UpdateOptions{})
			return err
		}}, nil
	case kindStatefulSet:
		object, err := c.k8sClient.AppsV1().StatefulSets(ref.namespace).Get(ctx, ref.name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return &workload{template: &object.Spec.Template, update: func(ctx context.Context) error {
			_, err := c.k8sClient.AppsV1().StatefulSets(ref.namespace).Update(ctx, object, metav1.UpdateOptions{})
			return err
		}}, nil
	case kindDaemonSet:
		object, err := c.k8sClient.AppsV1().DaemonSets(ref.namespace).Get(ctx, ref.name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return &workload{template: &object.Spec.Template, update: func(ctx context.Context) error {
			_, err := c.k8sClient.AppsV1().DaemonSets(ref.namespace).Update(ctx, object, metav1.UpdateOptions{})
			return err
		}}, nil
	default:
		return nil, fmt.Errorf("unsupported workload kind %q", ref.kind)
	}
}

func (c *Command) eligibleNamespaces(ctx context.Context) (map[string]struct{}, error) {
	selector := labels.Everything()
	if strings.TrimSpace(c.flagNamespaceSelector) != "" {
		var configured metav1.LabelSelector
		if err := json.Unmarshal([]byte(c.flagNamespaceSelector), &configured); err != nil {
			return nil, fmt.Errorf("decoding namespace selector: %w", err)
		}
		var err error
		selector, err = metav1.LabelSelectorAsSelector(&configured)
		if err != nil {
			return nil, fmt.Errorf("building namespace selector: %w", err)
		}
	}

	items, err := c.k8sClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing Namespaces: %w", err)
	}
	allow := mapset.NewSet()
	for _, namespace := range c.flagAllowNamespaces {
		allow.Add(namespace)
	}
	deny := mapset.NewSet()
	for _, namespace := range c.flagDenyNamespaces {
		deny.Add(namespace)
	}

	eligible := make(map[string]struct{})
	for _, namespace := range items.Items {
		if namespace.Name == metav1.NamespaceSystem || namespace.Name == metav1.NamespacePublic {
			continue
		}
		// Never mutate the Consul components installed by this release. With
		// connectInject.default=true they would otherwise look mesh-injectable
		// and DECOMMISSION would roll out the control plane itself.
		if c.flagReleaseNamespace != "" && namespace.Name == c.flagReleaseNamespace {
			continue
		}
		if common.ShouldIgnore(namespace.Name, deny, allow) {
			continue
		}
		if !selector.Matches(labels.Set(namespace.Labels)) {
			continue
		}
		eligible[namespace.Name] = struct{}{}
	}
	return eligible, nil
}

func (c *Command) convertWorkload(ctx context.Context, ref workloadRef) (bool, error) {
	changed := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Reset on every attempt so a conflict retry cannot report a change
		// that was computed against the object version that lost the race.
		changed = false

		target, err := c.getWorkload(ctx, ref)
		if err != nil {
			return err
		}

		inject, err := shouldInject(*target.template, c.flagDefaultInject)
		if err != nil {
			return fmt.Errorf("checking injection eligibility for %s: %w", ref, err)
		}
		if !inject {
			return nil
		}

		var selectedPort string
		switch c.flagStrategy {
		case conversionStrategyTranslate:
			selectedPort, changed, err = translateTemplate(target.template)
		case conversionStrategyDecommission:
			changed = decommissionTemplate(target.template)
		}
		if err != nil {
			return fmt.Errorf("converting %s with strategy %s: %w", ref, c.flagStrategy, err)
		}
		if !changed {
			return nil
		}

		if err := target.update(ctx); err != nil {
			return err
		}
		c.logger.Info("updated workload for multi-port conversion",
			"strategy", c.flagStrategy,
			"kind", string(ref.kind),
			"namespace", ref.namespace,
			"name", ref.name,
			"selected_port", selectedPort,
		)
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("updating %s: %w", ref, err)
	}
	return changed, nil
}

func shouldInject(template corev1.PodTemplateSpec, defaultInject bool) (bool, error) {
	raw, ok := template.Annotations[constants.AnnotationInject]
	if !ok {
		return defaultInject, nil
	}
	inject, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s annotation %q", constants.AnnotationInject, raw)
	}
	return inject, nil
}

func translateTemplate(template *corev1.PodTemplateSpec) (selected string, changed bool, err error) {
	if hasMultipleConnectServices(template.Annotations[constants.AnnotationService]) {
		return "", false, nil
	}

	candidates := candidatePorts(*template)
	if len(candidates) <= 1 {
		return "", false, nil
	}

	selected, err = selectedPort(*template, candidates)
	if err != nil {
		return "", false, err
	}
	if template.Annotations == nil {
		template.Annotations = make(map[string]string)
	}
	if template.Annotations[constants.AnnotationPort] == selected {
		return selected, false, nil
	}
	template.Annotations[constants.AnnotationPort] = selected
	return selected, true, nil
}

func decommissionTemplate(template *corev1.PodTemplateSpec) bool {
	if template.Annotations == nil {
		template.Annotations = make(map[string]string)
	}
	if template.Annotations[constants.AnnotationInject] == "false" {
		return false
	}
	template.Annotations[constants.AnnotationInject] = "false"
	return true
}

type applicationPort struct {
	token string
	value int32
}

// candidatePorts returns the ports a workload would register in Consul, using
// the same precedence the connect-inject webhook applies:
//
//   - an explicit consul.hashicorp.com/connect-service-port list wins;
//   - otherwise the webhook defaults the annotation from the first container;
//   - otherwise the first container declares no ports, and the registration is
//     derived from ports declared elsewhere in the Pod.
func candidatePorts(template corev1.PodTemplateSpec) []applicationPort {
	if raw, ok := template.Annotations[constants.AnnotationPort]; ok {
		if nonEmptyValueCount(raw) <= 1 {
			return nil
		}
		return annotatedPorts(raw, template.Spec.Containers)
	}
	if ports := containerPorts(firstContainer(template.Spec.Containers)...); len(ports) > 0 {
		return ports
	}
	return containerPorts(template.Spec.Containers...)
}

// annotatedPorts resolves each annotation token to a declared container port.
// common.PortValue matches a named port against every container in the Pod, so
// token resolution must do the same. A token that names no declared port is
// still a valid numeric selection and is kept verbatim.
func annotatedPorts(raw string, containers []corev1.Container) []applicationPort {
	declared := containerPorts(containers...)
	ports := make([]applicationPort, 0, len(declared))
	for _, item := range strings.Split(raw, ",") {
		token := strings.TrimSpace(item)
		if token == "" {
			continue
		}
		resolved := applicationPort{token: token}
		for _, port := range declared {
			if port.token == token {
				resolved.value = port.value
				break
			}
		}
		if resolved.value == 0 {
			if parsed, err := strconv.ParseInt(token, 10, 32); err == nil {
				resolved.value = int32(parsed)
			}
		}
		ports = append(ports, resolved)
	}
	return ports
}

func firstContainer(containers []corev1.Container) []corev1.Container {
	if len(containers) == 0 {
		return nil
	}
	return containers[:1]
}

func containerPorts(containers ...corev1.Container) []applicationPort {
	ports := make([]applicationPort, 0)
	for _, container := range containers {
		for _, port := range container.Ports {
			if port.Name == "" && port.ContainerPort <= 0 {
				continue
			}
			token := port.Name
			if token == "" {
				token = strconv.Itoa(int(port.ContainerPort))
			}
			ports = append(ports, applicationPort{token: token, value: port.ContainerPort})
		}
	}
	return ports
}

// selectedPort chooses the single port a converted workload will register.
//
// This mirrors the endpoints reconciler's pre-existing behavior rather than
// introducing a new rule: defaultPortTokenIndexForRegistration already resolves
// the default-port annotation by token name or numeric value and falls back to
// the first port, and the reconciler already registers only that port whenever
// it cannot register a port list. The conversion records the same choice in the
// pod template so the spec and the registration agree.
//
// The one divergence is deliberate. An unmatched annotation is an error here,
// while the reconciler silently falls back to the first port; a one-shot Job
// must not permanently record a port the operator did not choose.
func selectedPort(template corev1.PodTemplateSpec, ports []applicationPort) (string, error) {
	desired := strings.TrimSpace(template.Annotations[constants.AnnotationDefaultPort])
	if desired == "" {
		return ports[0].token, nil
	}

	desiredValue, desiredErr := strconv.ParseInt(desired, 10, 32)
	for _, port := range ports {
		if desired == port.token || (desiredErr == nil && int32(desiredValue) == port.value) {
			return port.token, nil
		}
	}
	return "", fmt.Errorf("configured default port %q is not one of the service ports this workload would register", desired)
}

func hasMultipleConnectServices(value string) bool {
	return nonEmptyValueCount(value) > 1
}

func nonEmptyValueCount(value string) int {
	count := 0
	for _, item := range strings.Split(value, ",") {
		if strings.TrimSpace(item) != "" {
			count++
		}
	}
	return count
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Apply a multi-port service registration conversion policy to workloads."

const help = `
Usage: consul-k8s-control-plane convert-multiport-services [options]

  Applies TRANSLATE or DECOMMISSION to mesh-eligible Kubernetes Deployments,
  StatefulSets, and DaemonSets. This command is intended for the Consul Helm
  post-upgrade hook.
`
