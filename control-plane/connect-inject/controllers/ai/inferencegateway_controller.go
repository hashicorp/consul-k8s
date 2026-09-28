// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ai

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	igwcache "github.com/hashicorp/consul-k8s/control-plane/connect-inject/controllers/ai/cache"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

const (
	inferenceGatewayFinalizer = "inference-gateway-exists-finalizer.consul.hashicorp.com"

	// conditionTypePoolResolved is set to True when the referenced
	// InferencePoolConfig exists, False when it is missing.
	conditionTypePoolResolved = "PoolResolved"

	reasonPoolNotFound = "PoolNotFound"
	reasonPoolNotReady = "PoolNotReady"
	reasonPoolResolved = "PoolResolved"

	// inferenceGatewayServicePort is the default Kubernetes Service port exposed
	// to callers in the mesh. Matches the convention used by other inference
	// gateway deployments in this cluster (8443).
	inferenceGatewayServicePort = int32(8443)

	// inferenceGatewayMetricsPort is the Prometheus /metrics port
	// (consul-inference-gateway default: -metrics-addr=:9090).
	inferenceGatewayMetricsPort = int32(9090)

	// labelManagedBy is stamped on every K8s resource owned by this controller.
	labelManagedBy = "consul.hashicorp.com/managed-by"
)

// InferenceGatewayController reconciles InferenceGateway objects.
//
// For each InferenceGateway it:
//  1. Ensures a finalizer is present.
//  2. Resolves the referenced InferencePoolConfig.
//  3. Creates or updates a Deployment and a ClusterIP Service (owned via
//     ownerReference so K8s GC removes them on deletion).
//  4. Upserts an AIGateway config entry in Consul so that the Consul
//     service-mesh control-plane can configure the ext_proc filter and
//     routing rules — following the same pattern as ConfigEntryController.
//  5. Writes PoolResolved, Available, and Ready status conditions.
//  6. On deletion: deletes the Consul config entry (with datacenter-ownership
//     guard), then drops the finalizer (owned K8s resources are GC'd).
//
// Out-of-band Consul mutations (direct consul config delete) are detected by a
// background Cache that runs a blocking-query long-poll against the ai-gateway
// kind. Any change triggers a Reconcile via a source.Channel subscription,
// mirroring api-gateway/controllers/gateway_controller.go:536.
type InferenceGatewayController struct {
	client.Client
	Log      logr.Logger
	Recorder record.EventRecorder

	// GatewayImage is the container image for the inference-gateway binary
	// (ext_proc sidecar). Injected at startup via v1controllers.go.
	GatewayImage string

	// DataplaneImage is the container image for consul-dataplane (Envoy).
	// Used as the main container in the inference-gateway pod, mirroring
	// the mesh-gateway deployment model. Sourced from c.flagConsulDataplaneImage.
	DataplaneImage string

	// ConsulK8SImage is the image for consul-k8s-control-plane, used for the
	// connect-init init container that writes the proxy-id file.
	// Sourced from c.flagConsulK8sImage.
	ConsulK8SImage string

	// DefaultResources is the fallback resource requests/limits for the
	// gateway container, sourced from ai.inferenceGateway.defaults.resources
	// in values.yaml. Applied when spec.resources is nil on the CRD object.
	DefaultResources corev1.ResourceRequirements

	// DefaultService is the fallback Service configuration sourced from
	// ai.inferenceGateway.defaults.service in values.yaml.
	// Applied when spec.service is nil on the CRD object.
	DefaultService v1alpha1.InferenceGatewayService

	// ConsulClientConfig is the Consul API client configuration.
	// GRPCPort, HTTPPort, and APITimeout are read from here for the init container
	// and consul-dataplane env vars.
	ConsulClientConfig *consul.Config

	// ConsulServerConnMgr is the watcher for the live Consul server address.
	ConsulServerConnMgr consul.ServerConnectionManager

	// ConsulAddress is the stable DNS name of the Consul server, e.g.
	// "release-consul-server.default.svc". Injected into the connect-init
	// init container as CONSUL_ADDRESSES and into consul-dataplane as -addresses.
	// Sourced from c.consul.Addresses in v1controllers.go.
	ConsulAddress string

	// ConsulTLSEnabled indicates whether the Consul server requires TLS.
	// When true the init container and consul-dataplane receive the CA cert and
	// server name; when false -tls-disabled is passed to consul-dataplane.
	ConsulTLSEnabled bool

	// ConsulCACert is the PEM-encoded CA certificate used to verify the Consul
	// server's TLS certificate. Only used when ConsulTLSEnabled is true.
	ConsulCACert string

	// ConsulTLSServerName is the SNI/TLS server name for Consul. Only used when
	// ConsulTLSEnabled is true.
	ConsulTLSServerName string

	// ConsulPartition is the Consul admin partition (Enterprise only; empty for OSS).
	ConsulPartition string

	// ConsulNamespace is the destination Consul namespace for the config entry.
	// Only used when EnableConsulNamespaces is true (Consul Enterprise).
	// On OSS this must remain empty so no ?ns= query param is ever sent.
	ConsulNamespace string

	// EnableConsulNamespaces indicates that the connected Consul server is
	// Consul Enterprise with namespaces enabled. When false (OSS), the
	// Namespace field is never populated on WriteOptions, QueryOptions, or
	// the config entry itself — OSS Consul rejects the ?ns= query parameter
	// with HTTP 400. Mirrors ConfigEntryController.EnableConsulNamespaces.
	EnableConsulNamespaces bool

	// Datacenter is the Consul datacenter name stamped as metadata on every
	// config entry so ownership can be asserted at delete time.
	Datacenter string

	// AuthMethod is the name of the Kubernetes auth method configured in Consul
	// for ACL login. When set, connect-init uses CONSUL_LOGIN_* env vars to
	// exchange the pod's ServiceAccount JWT for a Consul ACL token before
	// registering. Must match the auth method created by the Helm chart (usually
	// "<release>-k8s-auth-method"). Empty means ACLs are disabled.
	AuthMethod string

	// EnableK8SNSMirroring indicates that Kubernetes namespaces are mirrored
	// into Consul namespaces. When true and EnableConsulNamespaces is true,
	// the login namespace is "default" (the auth method lives there).
	// Mirrors the same field on MeshWebhook.
	EnableK8SNSMirroring bool

	// cache is the Consul ai-gateway long-poll cache. Initialised by
	// SetupWithManager; used to detect out-of-band Consul mutations.
	cache *igwcache.Cache
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=inferencegateways,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=inferencegateways/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=inferencegateways/finalizers,verbs=update
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=inferencepoolconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create

func (r *InferenceGatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("inferenceGateway", req.NamespacedName)
	log.Info("reconcile started")

	// ── Fetch the InferenceGateway ───────────────────────────────────────────
	igw := &v1alpha1.InferenceGateway{}
	if err := r.Client.Get(ctx, req.NamespacedName, igw); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("InferenceGateway not found; must have been deleted, nothing to do")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get InferenceGateway")
		return ctrl.Result{}, err
	}

	log.Info("fetched InferenceGateway",
		"resourceVersion", igw.ResourceVersion,
		"generation", igw.Generation,
		"poolRef", igw.Spec.PoolRef.Name,
	)

	// ── Build a per-reconcile Consul API client ───────────────────────────────
	// Mirrors the pattern used by ConfigEntryController: create a fresh client
	// from the connection manager's current server address so we automatically
	// pick up leader changes and token rotations.
	serverState, err := r.ConsulServerConnMgr.State()
	if err != nil {
		log.Error(err, "failed to get Consul server state")
		return ctrl.Result{}, err
	}
	consulClient, err := consul.NewClientFromConnMgrState(r.ConsulClientConfig, serverState)
	if err != nil {
		log.Error(err, "failed to create Consul API client")
		return ctrl.Result{}, err
	}

	// ── 1. Normal path: ensure finalizer ─────────────────────────────────────
	if igw.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(igw, inferenceGatewayFinalizer) {
			controllerutil.AddFinalizer(igw, inferenceGatewayFinalizer)
			if err := r.Client.Update(ctx, igw); err != nil {
				if k8serrors.IsConflict(err) {
					log.Info("conflict adding finalizer, requeueing")
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "failed to add finalizer")
				r.Recorder.Eventf(igw, corev1.EventTypeWarning, eventReasonFinalizerAdded, "failed to add finalizer: %v", err)
				return ctrl.Result{}, err
			}
			log.Info("finalizer added", "finalizer", inferenceGatewayFinalizer)
			r.Recorder.Event(igw, corev1.EventTypeNormal, eventReasonFinalizerAdded, "finalizer added successfully")
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// ── 2. Deletion path ──────────────────────────────────────────────────────
	// Owned Deployment + Service are GC'd by K8s via ownerReferences.
	// We only need to delete the Consul config entry and drop the finalizer.
	if !igw.DeletionTimestamp.IsZero() {
		log.Info("InferenceGateway marked for deletion",
			"deletionTimestamp", igw.DeletionTimestamp,
		)
		if controllerutil.ContainsFinalizer(igw, inferenceGatewayFinalizer) {
			if err := r.deleteConfigEntry(ctx, consulClient, igw, log); err != nil {
				log.Error(err, "failed to delete Consul config entry")
				r.Recorder.Eventf(igw, corev1.EventTypeWarning, eventReasonFinalizerRemoved,
					"failed to delete Consul config entry: %v", err)
				return ctrl.Result{RequeueAfter: 10 * time.Second}, err
			}
			controllerutil.RemoveFinalizer(igw, inferenceGatewayFinalizer)
			if err := r.Client.Update(ctx, igw); err != nil {
				if k8serrors.IsConflict(err) {
					log.Info("conflict removing finalizer, requeueing")
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "failed to remove finalizer")
				r.Recorder.Eventf(igw, corev1.EventTypeWarning, eventReasonFinalizerRemoved,
					"failed to remove finalizer: %v", err)
				return ctrl.Result{}, err
			}
			log.Info("finalizer removed, deletion will proceed")
			r.Recorder.Event(igw, corev1.EventTypeNormal, eventReasonFinalizerRemoved,
				"finalizer removed, deletion will proceed")
		}
		return ctrl.Result{}, nil
	}

	// ── 3. Resolve the referenced InferencePoolConfig ─────────────────────────
	pool := &v1alpha1.InferencePoolConfig{}
	poolKey := types.NamespacedName{Name: igw.Spec.PoolRef.Name, Namespace: igw.Namespace}
	if err := r.Client.Get(ctx, poolKey, pool); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("referenced InferencePoolConfig not found", "poolRef", igw.Spec.PoolRef.Name)
			msg := fmt.Sprintf("InferencePoolConfig %q not found in namespace %q",
				igw.Spec.PoolRef.Name, igw.Namespace)
			// readyReplicas is 0 because we cannot proceed to reconcile the Deployment
			// when the pool is missing — there is no Deployment to read replicas from yet.
			if syncErr := r.syncGatewayStatus(ctx, igw, false, msg, 0); syncErr != nil {
				return ctrl.Result{RequeueAfter: 10 * time.Second}, syncErr
			}
			r.Recorder.Eventf(igw, corev1.EventTypeWarning, eventReasonSyncFailed,
				"InferencePoolConfig %q not found", igw.Spec.PoolRef.Name)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		log.Error(err, "failed to get InferencePoolConfig", "poolRef", igw.Spec.PoolRef.Name)
		return ctrl.Result{}, err
	}

	log.Info("resolved InferencePoolConfig",
		"poolRef", pool.Name,
		"poolEnabled", pool.Spec.Enabled,
	)

	// ── 4. Reconcile the ServiceAccount (must exist before the Deployment so
	// the pod can authenticate via the Consul ACL auth method binding rule).
	if err := r.reconcileServiceAccount(ctx, igw); err != nil {
		log.Error(err, "failed to reconcile ServiceAccount")
		r.Recorder.Eventf(igw, corev1.EventTypeWarning, eventReasonSyncFailed,
			"failed to reconcile ServiceAccount: %v", err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// ── 5. Reconcile the Deployment ───────────────────────────────────────────
	if err := r.reconcileDeployment(ctx, igw, pool); err != nil {
		log.Error(err, "failed to reconcile Deployment")
		r.Recorder.Eventf(igw, corev1.EventTypeWarning, eventReasonSyncFailed,
			"failed to reconcile Deployment: %v", err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// ── 6. Reconcile the Service ──────────────────────────────────────────────
	if err := r.reconcileService(ctx, igw); err != nil {
		log.Error(err, "failed to reconcile Service")
		r.Recorder.Eventf(igw, corev1.EventTypeWarning, eventReasonSyncFailed,
			"failed to reconcile Service: %v", err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// ── 6. Upsert the Consul AIGateway config entry ───────────────────────────
	// Uses ConfigEntries().Set() following the same pattern as
	// ConfigEntryController.ReconcileEntry — not the catalog API.
	if err := r.upsertConfigEntry(ctx, consulClient, igw, pool, log); err != nil {
		log.Error(err, "failed to upsert Consul config entry")
		r.Recorder.Eventf(igw, corev1.EventTypeWarning, eventReasonSyncFailed,
			"failed to upsert Consul config entry: %v", err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// ── 8. Read Deployment readyReplicas ─────────────────────────────────────
	// Fetched after reconcileDeployment so the Deployment is guaranteed to exist.
	var readyReplicas int32
	dep := &appsv1.Deployment{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: igw.Name, Namespace: igw.Namespace}, dep); err == nil {
		readyReplicas = dep.Status.ReadyReplicas
	}

	// ── 9. Sync status conditions ─────────────────────────────────────────────
	poolReady := pool.Spec.Enabled
	poolMsg := fmt.Sprintf("InferencePoolConfig %q resolved and enabled=true", pool.Name)
	if !poolReady {
		poolMsg = fmt.Sprintf("InferencePoolConfig %q resolved but enabled=false; pool is standing by", pool.Name)
	}
	if err := r.syncGatewayStatus(ctx, igw, poolReady, poolMsg, readyReplicas); err != nil {
		log.Error(err, "failed to sync status conditions")
		r.Recorder.Eventf(igw, corev1.EventTypeWarning, eventReasonSyncFailed,
			"failed to sync status: %v", err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	log.Info("reconcile complete",
		"poolRef", igw.Spec.PoolRef.Name,
		"poolEnabled", poolReady,
	)
	r.Recorder.Event(igw, corev1.EventTypeNormal, eventReasonSynced, "InferenceGateway synced successfully")
	return ctrl.Result{}, nil
}

// ── Consul config-entry management ───────────────────────────────────────────

// upsertConfigEntry writes an AIGateway config entry to Consul using
// ConfigEntries().Set(), mirroring the write path in ConfigEntryController.
func (r *InferenceGatewayController) upsertConfigEntry(
	ctx context.Context,
	consulClient *capi.Client,
	igw *v1alpha1.InferenceGateway,
	pool *v1alpha1.InferencePoolConfig,
	log logr.Logger,
) error {
	entry := r.toConsulConfigEntry(igw, pool)
	writeOpts := &capi.WriteOptions{
		Partition: r.ConsulPartition,
	}
	// Only set Namespace on Consul Enterprise — OSS rejects the ?ns= param (HTTP 400).
	if r.EnableConsulNamespaces && r.ConsulNamespace != "" {
		writeOpts.Namespace = r.ConsulNamespace
	}

	if _, _, err := consulClient.ConfigEntries().Set(entry, writeOpts); err != nil {
		// If the Consul server does not yet recognise ai-gateway entries (e.g. a
		// pre-release binary in CI), log a warning and continue rather than
		// blocking reconciliation. The entry will be written once the server is
		// upgraded to a build that includes the ai-gateway config entry kind.
		if isConsulUnknownKindErr(err) {
			log.Info("Consul server does not yet support ai-gateway config entries; skipping upsert",
				"name", igw.Name,
				"error", err.Error(),
			)
			return nil
		}
		return fmt.Errorf("ConfigEntries().Set for %q: %w", igw.Name, err)
	}

	log.Info("upserted AIGateway config entry in Consul",
		"name", igw.Name,
		"namespace", r.ConsulNamespace,
		"partition", r.ConsulPartition,
	)
	return nil
}

// deleteConfigEntry removes the AIGateway config entry from Consul.
// Mirrors the delete path in ConfigEntryController.ReconcileEntry:
//  1. Get the entry; if 404, treat as desired state (nothing to do).
//  2. Check datacenter ownership via Meta[DatacenterKey].
//  3. Delete only if this datacenter owns the entry.
func (r *InferenceGatewayController) deleteConfigEntry(
	ctx context.Context,
	consulClient *capi.Client,
	igw *v1alpha1.InferenceGateway,
	log logr.Logger,
) error {
	queryOpts := &capi.QueryOptions{
		Partition: r.ConsulPartition,
	}
	// Only set Namespace on Consul Enterprise — OSS rejects the ?ns= param (HTTP 400).
	if r.EnableConsulNamespaces && r.ConsulNamespace != "" {
		queryOpts.Namespace = r.ConsulNamespace
	}

	entry, _, err := consulClient.ConfigEntries().Get(capi.InferenceGateway, igw.Name, queryOpts)
	if err != nil {
		if isConsulNotFoundErr(err) {
			// Already gone — desired state, not an error.
			log.Info("Consul config entry not found during deletion (already removed)",
				"name", igw.Name,
			)
			return nil
		}
		return fmt.Errorf("ConfigEntries().Get for %q: %w", igw.Name, err)
	}

	// Only delete if this datacenter originally wrote the entry.
	if entry.GetMeta()[common.DatacenterKey] != r.Datacenter {
		log.Info("skipping config entry deletion: owned by a different datacenter",
			"name", igw.Name,
			"entryDatacenter", entry.GetMeta()[common.DatacenterKey],
			"localDatacenter", r.Datacenter,
		)
		return nil
	}

	writeOpts := &capi.WriteOptions{
		Partition: r.ConsulPartition,
	}
	// Only set Namespace on Consul Enterprise — OSS rejects the ?ns= param (HTTP 400).
	if r.EnableConsulNamespaces && r.ConsulNamespace != "" {
		writeOpts.Namespace = r.ConsulNamespace
	}
	if _, err := consulClient.ConfigEntries().Delete(capi.InferenceGateway, igw.Name, writeOpts); err != nil {
		return fmt.Errorf("ConfigEntries().Delete for %q: %w", igw.Name, err)
	}

	log.Info("deleted InferenceGateway config entry from Consul", "name", igw.Name)
	return nil
}

// toConsulConfigEntry builds a capi.InferenceGatewayConfigEntry from the InferenceGateway
// and its resolved InferencePoolConfig. Fields mapped:
//
//   - Processor.FailureMode — from pool.Spec.Processor (defaults to "open").
//   - Failover — from pool.Spec.Routing.Fallback only; all other routing
//     (MatchRules, Scoring, Retry, Timeout) lives in the Consul catalog, not here.
//   - PII — verbatim from pool.Spec.Policy.
//   - Observability — metrics and tracing pillars from pool.Spec.Observability.
//
// NOTE: StateStore and RateLimit mappings are commented out below pending the
// Consul API adding those fields back to InferenceGatewayConfigEntry.
func (r *InferenceGatewayController) toConsulConfigEntry(
	igw *v1alpha1.InferenceGateway,
	pool *v1alpha1.InferencePoolConfig,
) capi.ConfigEntry {
	entry := &capi.InferenceGatewayConfigEntry{
		Kind:      capi.InferenceGateway,
		Name:      igw.Name,
		Partition: r.ConsulPartition,
		Meta: map[string]string{
			common.DatacenterKey:      r.Datacenter,
			constants.MetaKeyKubeNS:   igw.Namespace,
			constants.MetaKeyKubeName: igw.Name,
		},
		// Default to "closed": reject if ext_proc is unreachable (safe for production).
		// Overridden below from pool.Spec.Processor when the operator sets failureMode.
		Processor: capi.InferenceGatewayProcessor{
			FailureMode: "closed",
		},
	}

	// Map pool.Spec.Processor → entry.Processor.
	if p := pool.Spec.Processor; p != nil {
		if p.FailureMode != "" {
			entry.Processor.FailureMode = p.FailureMode
		}
		entry.Processor.BodyModelRouting = p.BodyModelRouting
	}

	// Only set Namespace on Consul Enterprise — OSS Consul rejects the ?ns=
	// query parameter (HTTP 400 "Namespaces are a Consul Enterprise feature").
	if r.EnableConsulNamespaces && r.ConsulNamespace != "" {
		entry.Namespace = r.ConsulNamespace
	}

	// TODO: Uncomment when capi.InferenceGatewayConfigEntry re-adds StateStore and RateLimit.
	// These fields were removed from the Consul API schema; the CRD-side types
	// (InferencePoolStateStore, InferencePoolRateLimit) are retained so no pool
	// manifests need to change when the API fields return.
	//
	// Map pool.Spec.StateStore → capi.InferenceGatewayStateStore.
	// Required when RateLimit.Enabled=true.
	// if ss := pool.Spec.StateStore; ss != nil {
	// 	entry.StateStore = &capi.InferenceGatewayStateStore{
	// 		Service:       ss.Service,
	// 		LocalBindPort: ss.LocalBindPort,
	// 	}
	// }
	//
	// Map pool.Spec.RateLimit → capi.InferenceGatewayRateLimit.
	// if rl := pool.Spec.RateLimit; rl != nil {
	// 	entry.RateLimit = toConsulRateLimit(rl)
	// }

	// Map pool.Spec.Routing.Fallback → capi.InferenceGatewayFailover.
	// NOTE: Routing MatchRules, Scoring, Retry, Timeout are NOT on the config
	// entry — they are resolved from the Consul catalog at runtime.
	if routing := pool.Spec.Routing; routing != nil && routing.Fallback != nil {
		entry.Failover = &capi.InferenceGatewayFailover{
			RetryOn:       routing.Fallback.RetryOn,
			MaxTiers:      routing.Fallback.MaxTiers,
			PerTryTimeout: routing.Fallback.PerTryTimeout,
		}
	}

	// Map pool.Spec.Policy → PII on the entry.
	if p := pool.Spec.Policy; p != nil {
		if p.PII != nil {
			entry.PII = &capi.InferenceGatewayPII{
				Scope:               capi.InferenceGatewayPIIScope(p.PII.Scope),
				DefaultAction:       capi.InferenceGatewayPIIAction(p.PII.DefaultAction),
				StreamHoldbackBytes: p.PII.StreamHoldbackBytes,
			}
			if p.PII.Mask != nil {
				entry.PII.Mask = &capi.InferenceGatewayPIIMask{
					Char:     p.PII.Mask.Char,
					KeepLast: p.PII.Mask.KeepLast,
				}
			}
			for _, d := range p.PII.Detectors {
				entry.PII.Detectors = append(entry.PII.Detectors, capi.InferenceGatewayPIIDetector{
					Name:   piiDetectorName(d.Name),
					Regex:  d.Regex,
					Action: capi.InferenceGatewayPIIAction(d.Action),
				})
			}
		}
	}

	// Map pool.Spec.Observability → capi.InferenceGatewayObservability.
	// Each sub-pillar (Metrics, Tracing) is mapped only when present.
	if obs := pool.Spec.Observability; obs != nil {
		co := &capi.InferenceGatewayObservability{}

		if m := obs.Metrics; m != nil {
			co.Metrics = &capi.InferenceGatewayMetrics{
				Enabled: m.Enabled,
			}
			// SemconvSchema, CustomLabels: removed from the Consul API — the
			// running server rejects these keys with HTTP 400.
			if m.Prometheus != nil {
				port := m.Prometheus.Port // int from CRD
				co.Metrics.Prometheus = &capi.InferenceGatewayMetricsPrometheus{
					Port: &port, // *int required by Consul API (0 = disable endpoint)
				}
				// Path: removed from the Consul API — always served on /metrics.
			}
			if m.OTLP != nil {
				co.Metrics.OTLP = &capi.InferenceGatewayOTLPExport{
					Endpoint: otlpHostPort(m.OTLP.Endpoint),
					Insecure: m.OTLP.Insecure,
				}
			}
		}

		if tr := obs.Tracing; tr != nil {
			co.Tracing = &capi.InferenceGatewayTracing{
				Enabled:     tr.Enabled,
				SampleRatio: tr.SampleRatio,
			}
			if tr.OTLP != nil {
				co.Tracing.OTLP = &capi.InferenceGatewayOTLPExport{
					Endpoint: otlpHostPort(tr.OTLP.Endpoint),
					Insecure: tr.OTLP.Insecure,
				}
			}
		}

		entry.Observability = co
	}

	return entry
}

// TODO: Uncomment when capi.InferenceGatewayConfigEntry re-adds StateStore and RateLimit.
//
// toConsulRateLimit converts an InferencePoolRateLimit to *capi.InferenceGatewayRateLimit.
// func toConsulRateLimit(rl *v1alpha1.InferencePoolRateLimit) *capi.InferenceGatewayRateLimit {
// 	crl := &capi.InferenceGatewayRateLimit{
// 		Enabled:     rl.Enabled,
// 		Enforcement: rl.Enforcement,
// 		Mode:        rl.Mode,
// 		CountMode:   rl.CountMode,
// 		Dimensions:  rl.Dimensions,
// 		DegradeMode: rl.DegradeMode,
// 	}
//
// 	if rl.Default != nil {
// 		crl.Default = toConsulLimitPair(rl.Default)
// 	}
// 	if rl.Global != nil {
// 		crl.Global = toConsulLimitPair(rl.Global)
// 	}
//
// 	for _, tl := range rl.TierLimits {
// 		crl.TierLimits = append(crl.TierLimits, capi.InferenceGatewayTierLimit{
// 			Tier:                   tl.Tier,
// 			MaxCompletionTokensCap: tl.MaxCompletionTokensCap,
// 			Requests:               toConsulLimit(tl.Requests),
// 			Tokens:                 toConsulLimit(tl.Tokens),
// 		})
// 	}
//
// 	for _, ml := range rl.ModelLimits {
// 		crl.ModelLimits = append(crl.ModelLimits, capi.InferenceGatewayModelLimit{
// 			Model:    ml.Model,
// 			Requests: toConsulLimit(ml.Requests),
// 			Tokens:   toConsulLimit(ml.Tokens),
// 		})
// 	}
//
// 	for _, tb := range rl.TierBindings {
// 		crl.TierBindings = append(crl.TierBindings, capi.InferenceGatewayTierBinding{
// 			Tier:      tb.Tier,
// 			SPIFFEIDs: tb.SPIFFEIDs,
// 			Partition: tb.Partition,
// 			Namespace: tb.Namespace,
// 		})
// 	}
//
// 	return crl
// }
//
// func toConsulLimitPair(p *v1alpha1.InferencePoolLimitPair) *capi.InferenceGatewayLimitPair {
// 	if p == nil {
// 		return nil
// 	}
// 	return &capi.InferenceGatewayLimitPair{
// 		Requests: toConsulLimit(p.Requests),
// 		Tokens:   toConsulLimit(p.Tokens),
// 	}
// }
//
// func toConsulLimit(l *v1alpha1.InferencePoolLimit) *capi.InferenceGatewayLimit {
// 	if l == nil {
// 		return nil
// 	}
// 	return &capi.InferenceGatewayLimit{
// 		Count: int(l.Count),
// 		Unit:  normaliseWindow(l.Window),
// 	}
// }

// normaliseWindow converts the window field to the exact string the Consul
// AI Gateway rate-limit processor accepts: second | minute | hour | day.
//
// The CRD now validates the enum at admission time, but objects that were
// stored before the enum was added may contain Go-duration shorthand
// (e.g. "1s", "1m", "1h") or any other legacy value.  This function maps
// every known alias so the controller never sends a value that causes a
// Consul HTTP 500.
//
//	Mapping table:
//	  "s" / "1s"             → "second"
//	  "m" / "1m" / "min"     → "minute"   (Consul default)
//	  "h" / "1h" / "hr"      → "hour"
//	  "d" / "1d"             → "day"
//	  "" / unrecognised       → "minute"   (Consul default)
//	  already canonical       → unchanged
func normaliseWindow(w string) string {
	switch strings.ToLower(strings.TrimSpace(w)) {
	// Already canonical — pass through.
	case "second", "minute", "hour", "day":
		return strings.ToLower(strings.TrimSpace(w))
	// Go-duration shorthand and common aliases.
	case "s", "1s", "sec", "secs":
		return "second"
	case "m", "1m", "min", "mins":
		return "minute"
	case "h", "1h", "hr", "hrs":
		return "hour"
	case "d", "1d":
		return "day"
	default:
		// Unknown value — default to minute (Consul default) rather than
		// sending an invalid string that causes HTTP 500.
		return "minute"
	}
}

// piiDetectorName normalises a built-in PII detector name from the hyphenated
// form accepted by the CRD (e.g. "credit-card", "api-key") to the underscore
// form required by the Consul API (e.g. "credit_card", "api_key").
//
//	"credit-card" → "credit_card"
//	"api-key"     → "api_key"
//	"ssn"         → "ssn"  (no hyphens, unchanged)
func piiDetectorName(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// otlpHostPort strips any URL scheme (http:// or https://) from an OTLP
// endpoint string so the Consul API receives a bare host:port value.
// Consul's config-entry validation rejects full URLs with "too many colons
// in address". The CRD accepts full URLs for operator convenience; this
// function normalises them at the point of conversion.
//
//	"http://otel-collector:4318"  → "otel-collector:4318"
//	"https://otel-collector:4318" → "otel-collector:4318"
//	"otel-collector:4318"         → "otel-collector:4318"  (unchanged)
func otlpHostPort(endpoint string) string {
	if s := strings.TrimPrefix(endpoint, "https://"); s != endpoint {
		return s
	}
	return strings.TrimPrefix(endpoint, "http://")
}

// isConsulNotFoundErr returns true when the Consul API returned a 404 for a
// config entry lookup, or when the server returns an error indicating the kind
// is not registered (e.g. pre-release Consul binary used in tests).
// Mirrors the private isNotFoundErr in
// controllers/configentries/configentry_controller.go.
func isConsulNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "404") ||
		strings.Contains(msg, "invalid config entry kind")
}

// isConsulUnknownKindErr returns true when the Consul server rejected a write
// because it does not know the config entry kind (HTTP 400 with "invalid config
// entry kind" in the body). This happens when the operator is deployed against a
// Consul release that predates the ai-gateway entry type.
func isConsulUnknownKindErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "invalid config entry kind")
}

// ── Kubernetes child-resource reconciliation ──────────────────────────────────

// reconcileDeployment creates or patches the Deployment owned by igw.
func (r *InferenceGatewayController) reconcileDeployment(
	ctx context.Context,
	igw *v1alpha1.InferenceGateway,
	pool *v1alpha1.InferencePoolConfig,
) error {
	log := r.Log.WithValues("inferenceGateway", igw.Name, "namespace", igw.Namespace)

	// Resolve the effective ext_proc image: spec field takes precedence over
	// the controller-level default so users can pin a per-gateway image.
	gatewayImage := r.GatewayImage
	if igw.Spec.Image != "" {
		gatewayImage = igw.Spec.Image
	}

	// Resolve effective resources: spec-level overrides controller default.
	resources := r.DefaultResources
	if igw.Spec.Resources != nil {
		resources = *igw.Spec.Resources
	}

	// Resolve the Consul catalog service port — same precedence as reconcileService:
	// spec.service.ports[0] → DefaultService.ports[0] → 8443.
	servicePort := inferenceGatewayServicePort
	if igw.Spec.Service != nil && len(igw.Spec.Service.Ports) > 0 {
		servicePort = igw.Spec.Service.Ports[0].Port
	} else if len(r.DefaultService.Ports) > 0 {
		servicePort = r.DefaultService.Ports[0].Port
	}

	desired := deploymentFor(igw, pool, r.DataplaneImage, r.ConsulK8SImage, gatewayImage, servicePort, resources, deploymentConsulConfig{
		address:              r.ConsulAddress,
		grpcPort:             r.ConsulClientConfig.GRPCPort,
		httpPort:             r.ConsulClientConfig.HTTPPort,
		apiTimeout:           r.ConsulClientConfig.APITimeout,
		tlsEnabled:           r.ConsulTLSEnabled,
		caCert:               r.ConsulCACert,
		tlsServerName:        r.ConsulTLSServerName,
		authMethod:           r.AuthMethod,
		enableNamespaces:     r.EnableConsulNamespaces,
		enableNSMirroring:    r.EnableK8SNSMirroring,
		consulLoginNamespace: r.ConsulNamespace,
	})
	if err := controllerutil.SetControllerReference(igw, desired, r.Client.Scheme()); err != nil {
		return fmt.Errorf("setting owner reference on Deployment: %w", err)
	}

	existing := &appsv1.Deployment{}
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	err := r.Client.Get(ctx, key, existing)
	if k8serrors.IsNotFound(err) {
		log.Info("creating Deployment", "deployment", desired.Name)
		if err := r.Client.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating Deployment %q: %w", desired.Name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting Deployment %q: %w", desired.Name, err)
	}

	// ServiceAccountName is immutable in a pod template — if it has drifted
	// (e.g. Deployment was created before reconcileServiceAccount ran), we must
	// delete and recreate the Deployment so the new pods inherit the correct SA.
	if existing.Spec.Template.Spec.ServiceAccountName != desired.Spec.Template.Spec.ServiceAccountName {
		log.Info("ServiceAccountName changed, recreating Deployment",
			"deployment", existing.Name,
			"old", existing.Spec.Template.Spec.ServiceAccountName,
			"new", desired.Spec.Template.Spec.ServiceAccountName,
		)
		if err := r.Client.Delete(ctx, existing); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("deleting Deployment %q for ServiceAccountName update: %w", existing.Name, err)
		}
		if err := r.Client.Create(ctx, desired); err != nil {
			return fmt.Errorf("recreating Deployment %q: %w", desired.Name, err)
		}
		return nil
	}

	// Patch mutable fields: init containers, containers, volumes, replicas, labels,
	// and ServiceAccountName (kept in sync for new deployments).
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec.Replicas = desired.Spec.Replicas
	existing.Spec.Template.Spec.InitContainers = desired.Spec.Template.Spec.InitContainers
	existing.Spec.Template.Spec.Containers = desired.Spec.Template.Spec.Containers
	existing.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes
	existing.Spec.Template.Spec.ServiceAccountName = desired.Spec.Template.Spec.ServiceAccountName
	existing.Labels = desired.Labels
	if err := r.Client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patching Deployment %q: %w", existing.Name, err)
	}
	log.Info("Deployment reconciled", "deployment", existing.Name)
	return nil
}

// reconcileServiceAccount ensures a dedicated ServiceAccount named igw.Name
// exists in igw.Namespace. The pod template references it by name so the
// Consul ACL auth method binding rule (selector: serviceaccount.name==igw.Name)
// matches and the pod receives a token with the inference-gateway policy.
func (r *InferenceGatewayController) reconcileServiceAccount(
	ctx context.Context,
	igw *v1alpha1.InferenceGateway,
) error {
	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      igw.Name,
			Namespace: igw.Namespace,
			Labels:    gatewayLabels(igw),
		},
	}
	if err := controllerutil.SetControllerReference(igw, desired, r.Client.Scheme()); err != nil {
		return fmt.Errorf("setting owner reference on ServiceAccount: %w", err)
	}
	existing := &corev1.ServiceAccount{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if k8serrors.IsNotFound(err) {
		r.Log.Info("creating ServiceAccount", "serviceAccount", desired.Name, "namespace", desired.Namespace)
		if err := r.Client.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating ServiceAccount %q: %w", desired.Name, err)
		}
		return nil
	}
	return err
}

// reconcileService creates or patches the ClusterIP Service owned by igw.
func (r *InferenceGatewayController) reconcileService(
	ctx context.Context,
	igw *v1alpha1.InferenceGateway,
) error {
	log := r.Log.WithValues("inferenceGateway", igw.Name, "namespace", igw.Namespace)

	// Resolve effective service config: spec-level overrides controller default.
	svc := r.DefaultService
	if igw.Spec.Service != nil {
		svc = *igw.Spec.Service
	}
	// If neither spec nor default provided a port, fall back to the hardcoded constant.
	if len(svc.Ports) == 0 {
		svc.Ports = []v1alpha1.InferenceGatewayServicePort{{Port: inferenceGatewayServicePort}}
	}
	if svc.Type == "" {
		svc.Type = corev1.ServiceTypeClusterIP
	}

	desired := serviceFor(igw, svc)
	if err := controllerutil.SetControllerReference(igw, desired, r.Client.Scheme()); err != nil {
		return fmt.Errorf("setting owner reference on Service: %w", err)
	}

	existing := &corev1.Service{}
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	err := r.Client.Get(ctx, key, existing)
	if k8serrors.IsNotFound(err) {
		log.Info("creating Service", "service", desired.Name)
		if err := r.Client.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating Service %q: %w", desired.Name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting Service %q: %w", desired.Name, err)
	}

	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec.Ports = desired.Spec.Ports
	existing.Labels = desired.Labels
	if err := r.Client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patching Service %q: %w", existing.Name, err)
	}
	log.Info("Service reconciled", "service", existing.Name)
	return nil
}

// ── Resource builder functions ────────────────────────────────────────────────

// deploymentConsulConfig carries Consul connectivity settings that must be
// baked into the pod template (init container env vars + consul-dataplane args).
// These mirror the fields the mesh webhook injects for regular workloads.
type deploymentConsulConfig struct {
	address              string        // stable DNS name, e.g. "release-consul-server.default.svc"
	grpcPort             int           // Consul gRPC port (default 8502)
	httpPort             int           // Consul HTTP(S) port (default 8500/8501)
	apiTimeout           time.Duration // connect-init API timeout
	tlsEnabled           bool
	caCert               string // PEM CA cert (when TLS enabled)
	tlsServerName        string // SNI name (when TLS enabled)
	authMethod           string // Consul ACL auth method name; empty = ACLs off
	enableNamespaces     bool   // Consul Enterprise namespaces enabled
	enableNSMirroring    bool   // K8s → Consul namespace mirroring enabled
	consulLoginNamespace string // explicit Consul namespace for ACL login
}

// initContainerFor builds the connect-init init container for an InferenceGateway
// pod. It mirrors what the mesh webhook does for regular workloads in container_init.go:
// all Consul connectivity env vars are set explicitly since the webhook is bypassed.
func initContainerFor(igw *v1alpha1.InferenceGateway, image string, cc deploymentConsulConfig) corev1.Container {
	env := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
			},
		},
		{Name: "CONSUL_NODE_NAME", Value: "$(NODE_NAME)-virtual"},
		{Name: "CONSUL_ADDRESSES", Value: cc.address},
		{Name: "CONSUL_GRPC_PORT", Value: strconv.Itoa(cc.grpcPort)},
		{Name: "CONSUL_HTTP_PORT", Value: strconv.Itoa(cc.httpPort)},
		{Name: "CONSUL_API_TIMEOUT", Value: cc.apiTimeout.String()},
		// DP_ENVOY_READY_BIND_ADDRESS is read by connect-init to configure the
		// Envoy readiness endpoint address — matches the mesh-gateway init container.
		{
			Name: "DP_ENVOY_READY_BIND_ADDRESS",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			},
		},
	}
	if cc.tlsEnabled {
		env = append(env,
			corev1.EnvVar{Name: constants.UseTLSEnvVar, Value: "true"},
			corev1.EnvVar{Name: constants.CACertPEMEnvVar, Value: cc.caCert},
			corev1.EnvVar{Name: constants.TLSServerNameEnvVar, Value: cc.tlsServerName},
		)
	}
	// When ACLs are enabled connect-init must exchange the pod's ServiceAccount
	// JWT for a Consul ACL token before registering the proxy service.
	// Mirrors container_init.go's CONSUL_LOGIN_* block exactly:
	//   - namespace mirroring on  → login namespace = "default" (auth method lives there)
	//   - namespaces on, no mirror → login namespace = consulLoginNamespace (destination ns)
	//   - namespaces off (OSS)    → no CONSUL_LOGIN_NAMESPACE at all
	if cc.authMethod != "" {
		env = append(env,
			corev1.EnvVar{Name: "CONSUL_LOGIN_AUTH_METHOD", Value: cc.authMethod},
			corev1.EnvVar{Name: "CONSUL_LOGIN_BEARER_TOKEN_FILE", Value: "/var/run/secrets/kubernetes.io/serviceaccount/token"},
			corev1.EnvVar{Name: "CONSUL_LOGIN_META", Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)"},
		)
		if cc.enableNamespaces {
			if cc.enableNSMirroring {
				env = append(env, corev1.EnvVar{Name: "CONSUL_LOGIN_NAMESPACE", Value: "default"})
			} else {
				env = append(env, corev1.EnvVar{Name: "CONSUL_LOGIN_NAMESPACE", Value: cc.consulLoginNamespace})
			}
		}
	}
	volMounts := []corev1.VolumeMount{
		{
			Name:      "consul-service",
			MountPath: "/consul/service",
		},
		{
			// connect-init writes consul-ca.pem and the proxy-id here.
			// Mirrors the consul-connect-inject-data volume the mesh webhook adds.
			Name:      "consul-connect-inject-data",
			MountPath: "/consul/connect-inject",
		},
	}
	if cc.authMethod != "" {
		// The default projected SA token mounted at this path has a short TTL
		// (default 1 h) and is audience-bound to the API server. Consul's auth
		// method reads it to authenticate the pod.
		volMounts = append(volMounts, corev1.VolumeMount{
			Name:      "service-account-token",
			MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
			ReadOnly:  true,
		})
	}
	return corev1.Container{
		Name:  "connect-init",
		Image: image,
		Env:   env,
		Command: []string{
			"/bin/sh", "-ec",
			"exec consul-k8s-control-plane connect-init" +
				" -pod-name=${POD_NAME}" +
				" -pod-namespace=${POD_NAMESPACE}" +
				" -gateway-kind=inference-gateway" +
				" -proxy-id-file=/consul/service/proxy-id" +
				" -service-name=" + igw.Name,
		},
		VolumeMounts: volMounts,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: boolPtr(false),
			ReadOnlyRootFilesystem:   boolPtr(true),
			RunAsNonRoot:             boolPtr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}
}

// dataplaneArgsFor builds the consul-dataplane CLI args for an InferenceGateway pod.
// Mirrors the args the mesh webhook's getContainerSidecarArgs produces for regular workloads.
func dataplaneArgsFor(cc deploymentConsulConfig) []string {
	args := []string{
		"-addresses", cc.address,
		"-grpc-port=" + strconv.Itoa(cc.grpcPort),
		"-proxy-service-id-path=/consul/service/proxy-id",
		"-log-level=info",
		"-log-json=false",
		"-envoy-admin-bind-address=127.0.0.1",
		"-xds-bind-addr=127.0.0.1",
		"-graceful-addr=127.0.0.1",
		"-graceful-port=20600",
	}
	if cc.tlsEnabled {
		if cc.tlsServerName != "" {
			args = append(args, "-tls-server-name="+cc.tlsServerName)
		}
		if cc.caCert != "" {
			args = append(args, "-ca-certs="+constants.LegacyConsulCAFile)
		}
	} else {
		args = append(args, "-tls-disabled")
	}
	// When ACLs are enabled consul-dataplane must log in via the Kubernetes
	// auth method so it can obtain a Consul token for xDS. Mirrors the
	// args the mesh webhook adds in consul_dataplane_sidecar.go.
	if cc.authMethod != "" {
		args = append(args,
			"-credential-type=login",
			"-login-auth-method="+cc.authMethod,
			"-login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token",
		)
	}
	return args
}

// deploymentFor returns the desired Deployment for an InferenceGateway.
//
// The pod is modelled after the mesh-gateway deployment:
//   - connect-inject: "false" — the webhook is skipped; the controller builds
//     the pod template directly with all containers.
//   - gateway-kind: "inference-gateway" — isGateway() returns true so the
//     endpoints controller routes registration through registerGateway() →
//     createGatewayRegistrations(), which stamps ServiceKind=inference-gateway.
//   - Init container (consul-k8s-control-plane connect-init -gateway-kind=…)
//     writes the proxy-id to /consul/service/proxy-id.
//   - Main container: consul-dataplane reads proxy-id, connects to Consul gRPC,
//     and bootstraps Envoy as an inference-gateway proxy via xDS.
//   - Sidecar container: inference-gateway binary listens for ext_proc calls on
//     the UDS /run/consul/ext_proc.sock, shared via the run-consul emptyDir.
func deploymentFor(
	igw *v1alpha1.InferenceGateway,
	pool *v1alpha1.InferencePoolConfig,
	dataplaneImage string,
	consulK8SImage string,
	gatewayImage string,
	servicePort int32,
	resources corev1.ResourceRequirements,
	cc deploymentConsulConfig,
) *appsv1.Deployment {
	logLevel := igw.Spec.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}
	labels := gatewayLabels(igw)
	// The endpoints controller's registerGateway() only processes pods that
	// carry the ManagedByValue label, mirroring mesh-gateway-deployment.yaml.
	podLabels := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		podLabels[k] = v
	}
	podLabels[constants.KeyManagedBy] = constants.ManagedByValue

	replicas := int32(1)
	if igw.Spec.Replicas != nil {
		replicas = *igw.Spec.Replicas
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      igw.Name,
			Namespace: igw.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabels,
					Annotations: map[string]string{
						// Opt out of webhook injection — we manage all containers here.
						constants.AnnotationInject: "false",
						// Mark as a gateway so the endpoints controller uses registerGateway().
						constants.AnnotationGatewayKind:              "inference-gateway",
						constants.AnnotationGatewayConsulServiceName: igw.Name,
						// Consul catalog service port read by createGatewayRegistrations.
						constants.AnnotationInferenceGatewayPort: fmt.Sprintf("%d", servicePort),
					},
				},
				Spec: corev1.PodSpec{
					// ServiceAccountName must match igw.Name so the Consul ACL
					// auth method binding rule (selector: serviceaccount.name==igw.Name)
					// resolves the correct role and policy for this gateway.
					ServiceAccountName: igw.Name,
					// connect-init writes the proxy-id to /consul/service/proxy-id
					// so that consul-dataplane can bootstrap Envoy from Consul xDS.
					InitContainers: []corev1.Container{initContainerFor(igw, consulK8SImage, cc)},
					Containers: []corev1.Container{
						{
							// consul-dataplane boots Envoy from the proxy-id written by connect-init.
							// This is the primary process and runs as the inference-gateway proxy type.
							Name:    "consul-dataplane",
							Image:   dataplaneImage,
							Command: []string{"consul-dataplane"},
							Args:    dataplaneArgsFor(cc),
							Env: []corev1.EnvVar{
								{
									Name: "NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
									},
								},
								{
									Name:  "DP_SERVICE_NODE_NAME",
									Value: "$(NODE_NAME)-virtual",
								},
								{
									Name: "POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
									},
								},
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
									},
								},
								{
									Name: "HOST_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
									},
								},
								{
									// consul-dataplane has readOnlyRootFilesystem:true so it needs
									// TMPDIR to point at a writable volume. run-consul is already
									// mounted read-write on this container.
									Name:  "TMPDIR",
									Value: "/run/consul",
								},
							},
							VolumeMounts: func() []corev1.VolumeMount {
								mounts := []corev1.VolumeMount{
									{
										// Read-only: consul-dataplane only reads the proxy-id written
										// by connect-init; it never writes to this volume.
										Name:      "consul-service",
										MountPath: "/consul/service",
										ReadOnly:  true,
									},
									{
										// consul-dataplane reads the CA cert and proxy token
										// written by connect-init into this shared directory.
										Name:      "consul-connect-inject-data",
										MountPath: "/consul/connect-inject",
									},
									{
										// Envoy needs the UDS directory to reach the ext_proc sidecar.
										Name:      "run-consul",
										MountPath: "/run/consul",
									},
								}
								if cc.authMethod != "" {
									mounts = append(mounts, corev1.VolumeMount{
										Name:      "service-account-token",
										MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
										ReadOnly:  true,
									})
								}
								return mounts
							}(),
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: boolPtr(false),
								ReadOnlyRootFilesystem:   boolPtr(true),
								RunAsNonRoot:             boolPtr(true),
								Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
							},
						},
						{
							// inference-gateway is the ext_proc sidecar that Envoy calls over UDS.
							// It only handles LLM request/response processing — it does not run Envoy.
							Name:  "inference-gateway",
							Image: gatewayImage,
							Args: []string{
								"-uds-path=/run/consul/ext_proc.sock",
								"-config-entry=" + igw.Name,
								"-consul-http-addr=http://" + fmt.Sprintf("%s:%d", cc.address, cc.httpPort),
								"-log-level=$(CONSUL_IGW_LOG_LEVEL)",
							},
							Ports: []corev1.ContainerPort{
								{Name: "metrics", ContainerPort: inferenceGatewayMetricsPort, Protocol: corev1.ProtocolTCP},
							},
							Env: []corev1.EnvVar{
								{Name: "POOL_NAME", Value: pool.Name},
								{Name: "POOL_NAMESPACE", Value: pool.Namespace},
								// Log level for the inference-gateway process.
								// Set via spec.logLevel on the InferenceGateway CR.
								// Valid values: debug, info, warn, error (default: info).
								{Name: "CONSUL_IGW_LOG_LEVEL", Value: logLevel},
							},
							Resources: resources,
							VolumeMounts: []corev1.VolumeMount{{
								Name:      "run-consul",
								MountPath: "/run/consul",
							}},
						},
					},
					Volumes: func() []corev1.Volume {
						vols := []corev1.Volume{
							{
								// proxy-id file shared between connect-init and consul-dataplane.
								Name: "consul-service",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
								},
							},
							{
								// Shared writable directory for connect-init (writes CA cert,
								// proxy token) and consul-dataplane (reads them).
								// Mirrors the consul-connect-inject-data volume the mesh webhook adds.
								Name: "consul-connect-inject-data",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
								},
							},
							{
								// UDS socket directory shared between consul-dataplane (Envoy) and
								// the inference-gateway ext_proc sidecar.
								Name: "run-consul",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
						}
						if cc.authMethod != "" {
							// Projected ServiceAccount token used by connect-init and
							// consul-dataplane to authenticate with Consul's ACL auth method.
							expirationSeconds := int64(86400)
							vols = append(vols, corev1.Volume{
								Name: "service-account-token",
								VolumeSource: corev1.VolumeSource{
									Projected: &corev1.ProjectedVolumeSource{
										Sources: []corev1.VolumeProjection{{
											ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
												Path:              "token",
												ExpirationSeconds: &expirationSeconds,
											},
										}},
									},
								},
							})
						}
						return vols
					}(),
				},
			},
		},
	}
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }

// serviceFor returns the desired Service for an InferenceGateway.
func serviceFor(igw *v1alpha1.InferenceGateway, svc v1alpha1.InferenceGatewayService) *corev1.Service {
	labels := gatewayLabels(igw)

	ports := make([]corev1.ServicePort, 0, len(svc.Ports))
	for i, p := range svc.Ports {
		name := "grpc"
		if i > 0 {
			name = fmt.Sprintf("grpc-%d", i)
		}
		ports = append(ports, corev1.ServicePort{
			Name:       name,
			Port:       p.Port,
			TargetPort: intstr.FromInt32(p.Port),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      igw.Name,
			Namespace: igw.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     svc.Type,
			Selector: labels,
			Ports:    ports,
		},
	}
}

// gatewayLabels returns the standard label set applied to all K8s resources
// owned by a given InferenceGateway.
func gatewayLabels(igw *v1alpha1.InferenceGateway) map[string]string {
	return map[string]string{
		"app":          igw.Name,
		labelManagedBy: "consul-connect-inject",
	}
}

// ── Status sync ───────────────────────────────────────────────────────────────

// syncGatewayStatus writes PoolResolved, Available, and Ready conditions and
// readyReplicas onto the InferenceGateway status sub-resource.
// readyReplicas is sourced from the owned Deployment's Status.ReadyReplicas
// so consumers can see pod readiness without querying the Deployment directly.
func (r *InferenceGatewayController) syncGatewayStatus(
	ctx context.Context,
	igw *v1alpha1.InferenceGateway,
	poolReady bool,
	poolMsg string,
	readyReplicas int32,
) error {
	log := r.Log.WithValues("inferenceGateway", igw.Name, "namespace", igw.Namespace)
	now := metav1.Now()

	poolResolvedStatus := metav1.ConditionTrue
	poolResolvedReason := reasonPoolResolved
	if !poolReady {
		poolResolvedStatus = metav1.ConditionFalse
		poolResolvedReason = reasonPoolNotReady
	}

	availableStatus := poolResolvedStatus
	availableMsg := "InferenceGateway Deployment, Service, and Consul config entry are provisioned"
	if !poolReady {
		availableMsg = "InferenceGateway is not available; backing pool is not ready"
	}

	readyStatus := poolResolvedStatus
	readyMsg := "InferenceGateway is reconciled; pool is resolved, enabled, and config entry written to Consul"
	if !poolReady {
		readyMsg = "InferenceGateway is not ready; backing InferencePoolConfig is not ready"
	}

	log.Info("setting status conditions",
		"PoolResolved", poolResolvedStatus,
		"Available", availableStatus,
		"Ready", readyStatus,
		"readyReplicas", readyReplicas,
	)

	conditions := []metav1.Condition{
		{
			Type:               conditionTypePoolResolved,
			Status:             poolResolvedStatus,
			ObservedGeneration: igw.Generation,
			LastTransitionTime: now,
			Reason:             poolResolvedReason,
			Message:            poolMsg,
		},
		{
			Type:               "Available",
			Status:             availableStatus,
			ObservedGeneration: igw.Generation,
			LastTransitionTime: now,
			Reason:             reasonReconciled,
			Message:            availableMsg,
		},
		{
			Type:               conditionTypeReady,
			Status:             readyStatus,
			ObservedGeneration: igw.Generation,
			LastTransitionTime: now,
			Reason:             reasonReconciled,
			Message:            readyMsg,
		},
	}

	patch := client.MergeFrom(igw.DeepCopy())
	igw.Status.Conditions = mergeConditions(igw.Status.Conditions, conditions)
	igw.Status.ReadyReplicas = readyReplicas
	igw.Status.LastSyncedTime = &now

	if err := r.Client.Status().Patch(ctx, igw, patch); err != nil {
		log.Error(err, "failed to patch status")
		return err
	}

	log.Info("status conditions patched successfully",
		"readyReplicas", readyReplicas,
		"lastSyncedTime", now,
	)
	return nil
}

// ── Manager registration ──────────────────────────────────────────────────────

// transformConsulAIGateway maps a changed Consul ai-gateway config entry back
// to the K8s InferenceGateway NamespacedName. This is the TranslatorFn passed
// to cache.Subscribe — mirroring transformConsulGateway in
// api-gateway/controllers/gateway_controller.go:681.
func transformConsulAIGateway(entry capi.ConfigEntry) []types.NamespacedName {
	m := entry.GetMeta()
	kubeName := m[constants.MetaKeyKubeName]
	if kubeName == "" {
		return nil
	}
	return []types.NamespacedName{{
		Name:      kubeName,
		Namespace: m[constants.MetaKeyKubeNS],
	}}
}

// gatewaysForPool maps an InferencePoolConfig change to the InferenceGateway
// objects that reference it via spec.poolRef.name, so that updating a pool
// triggers reconciliation of every gateway that depends on it.
func (r *InferenceGatewayController) gatewaysForPool(ctx context.Context, obj client.Object) []ctrl.Request {
	pool, ok := obj.(*v1alpha1.InferencePoolConfig)
	if !ok {
		return nil
	}

	var list v1alpha1.InferenceGatewayList
	if err := r.Client.List(ctx, &list, client.InNamespace(pool.Namespace)); err != nil {
		r.Log.Error(err, "failed to list InferenceGateways for pool", "pool", pool.Name)
		return nil
	}

	var requests []ctrl.Request
	for _, igw := range list.Items {
		if igw.Spec.PoolRef.Name == pool.Name {
			requests = append(requests, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      igw.Name,
					Namespace: igw.Namespace,
				},
			})
		}
	}
	return requests
}

// SetupWithManager registers InferenceGatewayController with the controller-runtime
// manager and starts the background Consul long-poll cache.
//
// ctx must be the manager's root context so that the background cache goroutine
// is cancelled when the manager shuts down — matching the pattern in
// api-gateway/controllers/gateway_controller.go:SetupGatewayControllerWithManager.
func (r *InferenceGatewayController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.Log.Info("registering InferenceGatewayController with manager")

	// Build the Consul ai-gateway cache and start the background poll.
	// The goroutine exits when ctx is cancelled (manager shutdown).
	r.cache = igwcache.New(igwcache.Config{
		ConsulClientConfig:  r.ConsulClientConfig,
		ConsulServerConnMgr: r.ConsulServerConnMgr,
		Datacenter:          r.Datacenter,
		Logger:              r.Log.WithName("cache"),
	})
	go r.cache.Run(ctx)

	// Subscribe to ai-gateway cache events so out-of-band Consul mutations
	// (e.g. direct consul config delete) trigger a Reconcile.
	sub := r.cache.Subscribe(ctx, transformConsulAIGateway)

	return ctrl.NewControllerManagedBy(mgr).
		Named("inferencegateway").
		For(&v1alpha1.InferenceGateway{}).
		// Owned K8s resources re-trigger reconciliation when mutated externally.
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		// Re-reconcile any InferenceGateway whose spec.poolRef points to a
		// pool that just changed — e.g. enabled toggled, rate-limit updated.
		Watches(
			&v1alpha1.InferencePoolConfig{},
			handler.EnqueueRequestsFromMapFunc(r.gatewaysForPool),
		).
		// Re-reconcile when an ai-gateway config entry is mutated or deleted in
		// Consul out-of-band — same mechanism as api-gateway/controllers/
		// gateway_controller.go:536 with c.Subscribe(ctx, api.APIGateway, ...).
		WatchesRawSource(
			source.Channel(
				sub.Events(),
				&handler.EnqueueRequestForObject{},
			),
		).
		Complete(r)
}
