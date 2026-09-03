// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ai

import (
	"context"
	"fmt"
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

	// inferenceGatewayPort is the gRPC ext_proc port the gateway binary binds to
	// (consul-inference-gateway default: -addr=:9000).
	inferenceGatewayPort = int32(9000)

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

	// GatewayImage is the container image for the inference-gateway binary.
	// Injected at startup via v1controllers.go.
	GatewayImage string

	// ConsulClientConfig is the Consul API client configuration.
	ConsulClientConfig *consul.Config

	// ConsulServerConnMgr is the watcher for the live Consul server address.
	ConsulServerConnMgr consul.ServerConnectionManager

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

	// ── 4. Reconcile the Deployment ───────────────────────────────────────────
	if err := r.reconcileDeployment(ctx, igw, pool); err != nil {
		log.Error(err, "failed to reconcile Deployment")
		r.Recorder.Eventf(igw, corev1.EventTypeWarning, eventReasonSyncFailed,
			"failed to reconcile Deployment: %v", err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// ── 5. Reconcile the Service ──────────────────────────────────────────────
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

	entry, _, err := consulClient.ConfigEntries().Get(capi.AIGateway, igw.Name, queryOpts)
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
	if _, err := consulClient.ConfigEntries().Delete(capi.AIGateway, igw.Name, writeOpts); err != nil {
		return fmt.Errorf("ConfigEntries().Delete for %q: %w", igw.Name, err)
	}

	log.Info("deleted AIGateway config entry from Consul", "name", igw.Name)
	return nil
}

// toConsulConfigEntry builds a capi.AIGatewayConfigEntry from the InferenceGateway
// and its resolved InferencePoolConfig. It maps pool.Spec.Routing → AIGatewayRouting
// and pool.Spec.RateLimit → AIGatewayRateLimit, and stamps standard
// datacenter/kubernetes provenance metadata so deleteConfigEntry can assert
// ownership at delete time.
func (r *InferenceGatewayController) toConsulConfigEntry(
	igw *v1alpha1.InferenceGateway,
	pool *v1alpha1.InferencePoolConfig,
) capi.ConfigEntry {
	entry := &capi.AIGatewayConfigEntry{
		Kind:      capi.AIGateway,
		Name:      igw.Name,
		Partition: r.ConsulPartition,
		Meta: map[string]string{
			common.DatacenterKey:      r.Datacenter,
			constants.MetaKeyKubeNS:   igw.Namespace,
			constants.MetaKeyKubeName: igw.Name,
		},
		// ApplyTo binds this policy to the inference-gateway service whose name
		// matches the InferenceGateway resource.
		ApplyTo: []string{igw.Name},
	}

	// Only set Namespace on Consul Enterprise — OSS Consul rejects the ?ns=
	// query parameter (HTTP 400 "Namespaces are a Consul Enterprise feature").
	// Mirrors ConfigEntryController.EnableConsulNamespaces guard.
	if r.EnableConsulNamespaces && r.ConsulNamespace != "" {
		entry.Namespace = r.ConsulNamespace
	}

	// Map pool.Spec.StateStore → capi.AIGatewayStateStore.
	// Required when RateLimit.Enabled=true; Consul rejects the config entry
	// with HTTP 500 if rateLimit is enabled but StateStore.Service is empty.
	if ss := pool.Spec.StateStore; ss != nil {
		entry.StateStore = &capi.AIGatewayStateStore{
			Service:       ss.Service,
			LocalBindPort: ss.LocalBindPort,
		}
	}

	// Map pool.Spec.Routing → capi.AIGatewayRouting.
	if r := pool.Spec.Routing; r != nil {
		entry.Routing = toConsulRouting(r)
	}

	// Map pool.Spec.RateLimit → capi.AIGatewayRateLimit.
	if rl := pool.Spec.RateLimit; rl != nil {
		entry.RateLimit = toConsulRateLimit(rl)
	}

	return entry
}

// toConsulRouting converts an InferencePoolRouting to capi.AIGatewayRouting.
func toConsulRouting(r *v1alpha1.InferencePoolRouting) capi.AIGatewayRouting {
	routing := capi.AIGatewayRouting{
		FallbackChain:    r.FallbackChain,
		ConfigValidation: r.ConfigValidation,
	}

	for _, rule := range r.MatchRules {
		cr := capi.AIGatewayMatchRule{
			RequireCapabilities: rule.RequireCapabilities,
			Candidates:          rule.Candidates,
			FallbackChain:       rule.FallbackChain,
			When: capi.AIGatewayMatch{
				Path:    rule.When.Path,
				BodyHas: rule.When.BodyHas,
			},
		}
		if id := rule.When.Identity; id != nil {
			cr.When.Identity = &capi.AIGatewayIdentityMatch{
				Service:   id.Service,
				Partition: id.Partition,
				Namespace: id.Namespace,
			}
		}
		routing.MatchRules = append(routing.MatchRules, cr)
	}

	if len(r.ComplianceMap) > 0 {
		routing.ComplianceMap = make(map[string]capi.AIGatewayCompliance, len(r.ComplianceMap))
		for k, v := range r.ComplianceMap {
			routing.ComplianceMap[k] = capi.AIGatewayCompliance{
				AllowedRegions: v.AllowedRegions,
				// Note: InferencePoolCompliance.DenyModels has no direct
				// counterpart in capi.AIGatewayCompliance; it uses AllowedClusters.
			}
		}
	}

	if fb := r.Fallback; fb != nil {
		routing.Fallback = &capi.AIGatewayFallback{
			RetryOn:       fb.RetryOn,
			MaxTiers:      fb.MaxTiers,
			PerTryTimeout: fb.PerTryTimeout,
		}
	}

	if rt := r.Retry; rt != nil {
		routing.Retry = &capi.AIGatewayRetry{
			MaxAttempts: rt.MaxAttempts,
			RetryOn:     rt.RetryOn,
		}
	}

	if to := r.Timeout; to != nil {
		routing.Timeout = &capi.AIGatewayTimeout{
			Connect: to.Connect,
			Request: to.Request,
		}
	}

	if sc := r.Scoring; sc != nil {
		cs := &capi.AIGatewayScoring{
			Scorers: sc.Scorers,
		}
		for _, wt := range sc.WeightedSplit {
			cs.WeightedSplit = append(cs.WeightedSplit, capi.AIGatewayWeightedTarget{
				Cluster: wt.Cluster,
				Weight:  wt.Weight,
			})
		}
		routing.Scoring = cs
	}

	return routing
}

// toConsulRateLimit converts an InferencePoolRateLimit to *capi.AIGatewayRateLimit.
func toConsulRateLimit(rl *v1alpha1.InferencePoolRateLimit) *capi.AIGatewayRateLimit {
	crl := &capi.AIGatewayRateLimit{
		Enabled:     rl.Enabled,
		Enforcement: rl.Enforcement,
		Mode:        rl.Mode,
		CountMode:   rl.CountMode,
		Dimensions:  rl.Dimensions,
		DegradeMode: rl.DegradeMode,
	}

	if rl.Default != nil {
		crl.Default = toConsulLimitPair(rl.Default)
	}
	if rl.Global != nil {
		crl.Global = toConsulLimitPair(rl.Global)
	}

	for _, tl := range rl.TierLimits {
		crl.TierLimits = append(crl.TierLimits, capi.AIGatewayTierLimit{
			Tier:                   tl.Tier,
			MaxCompletionTokensCap: tl.MaxCompletionTokensCap,
			Requests:               toConsulLimit(tl.Requests),
			Tokens:                 toConsulLimit(tl.Tokens),
		})
	}

	for _, ml := range rl.ModelLimits {
		crl.ModelLimits = append(crl.ModelLimits, capi.AIGatewayModelLimit{
			Model:    ml.Model,
			Requests: toConsulLimit(ml.Requests),
			Tokens:   toConsulLimit(ml.Tokens),
		})
	}

	for _, tb := range rl.TierBindings {
		crl.TierBindings = append(crl.TierBindings, capi.AIGatewayTierBinding{
			Tier:      tb.Tier,
			SPIFFEIDs: tb.SPIFFEIDs,
			Partition: tb.Partition,
			Namespace: tb.Namespace,
		})
	}

	return crl
}

func toConsulLimitPair(p *v1alpha1.InferencePoolLimitPair) *capi.AIGatewayLimitPair {
	if p == nil {
		return nil
	}
	return &capi.AIGatewayLimitPair{
		Requests: toConsulLimit(p.Requests),
		Tokens:   toConsulLimit(p.Tokens),
	}
}

func toConsulLimit(l *v1alpha1.InferencePoolLimit) *capi.AIGatewayLimit {
	if l == nil {
		return nil
	}
	return &capi.AIGatewayLimit{
		Count: int(l.Count),
		Unit:  normaliseWindow(l.Window),
	}
}

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

	// Resolve the effective image: spec field takes precedence over the
	// controller-level default so users can pin a per-gateway image.
	image := r.GatewayImage
	if igw.Spec.Image != "" {
		image = igw.Spec.Image
	}

	desired := deploymentFor(igw, pool, image)
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

	// Patch mutable fields: init containers, containers, volumes, replicas, and labels.
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec.Replicas = desired.Spec.Replicas
	existing.Spec.Template.Spec.InitContainers = desired.Spec.Template.Spec.InitContainers
	existing.Spec.Template.Spec.Containers = desired.Spec.Template.Spec.Containers
	existing.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes
	existing.Labels = desired.Labels
	if err := r.Client.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("patching Deployment %q: %w", existing.Name, err)
	}
	log.Info("Deployment reconciled", "deployment", existing.Name)
	return nil
}

// reconcileService creates or patches the ClusterIP Service owned by igw.
func (r *InferenceGatewayController) reconcileService(
	ctx context.Context,
	igw *v1alpha1.InferenceGateway,
) error {
	log := r.Log.WithValues("inferenceGateway", igw.Name, "namespace", igw.Namespace)

	desired := serviceFor(igw)
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

// deploymentFor returns the desired Deployment for an InferenceGateway.
// The connect-inject webhook annotation is set to "true" so that the mesh webhook
// automatically injects the consul-dataplane sidecar and connect-init container,
// exactly as it does for any other mesh-enabled workload.
func deploymentFor(
	igw *v1alpha1.InferenceGateway,
	pool *v1alpha1.InferencePoolConfig,
	image string,
) *appsv1.Deployment {
	labels := gatewayLabels(igw)

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
					Labels: labels,
					Annotations: map[string]string{
						// Opt in to the connect-inject webhook so that the mesh
						// webhook automatically injects the consul-dataplane
						// sidecar and the consul-connect-inject-init init container.
						constants.AnnotationInject: "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "inference-gateway",
						Image: image,
						Args: []string{
							fmt.Sprintf("-addr=:%d", inferenceGatewayPort),
							fmt.Sprintf("-metrics-addr=:%d", inferenceGatewayMetricsPort),
							"-registry-file=/app/configs/inference-registry.yaml",
						},
						Ports: []corev1.ContainerPort{
							{Name: "grpc", ContainerPort: inferenceGatewayPort, Protocol: corev1.ProtocolTCP},
							{Name: "metrics", ContainerPort: inferenceGatewayMetricsPort, Protocol: corev1.ProtocolTCP},
						},
						Env: []corev1.EnvVar{
							{Name: "POOL_NAME", Value: pool.Name},
							{Name: "POOL_NAMESPACE", Value: pool.Namespace},
							{Name: "POOL_ENABLED", Value: fmt.Sprintf("%t", pool.Spec.Enabled)},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "inference-registry",
							MountPath: "/app/configs/inference-registry.yaml",
							SubPath:   "inference-registry.yaml",
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "inference-registry",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "inference-registry",
								},
							},
						},
					}},
				},
			},
		},
	}
}

// serviceFor returns the desired ClusterIP Service for an InferenceGateway.
func serviceFor(igw *v1alpha1.InferenceGateway) *corev1.Service {
	labels := gatewayLabels(igw)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      igw.Name,
			Namespace: igw.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports: []corev1.ServicePort{{
				Name:       "grpc",
				Port:       inferenceGatewayPort,
				TargetPort: intstr.FromInt32(inferenceGatewayPort),
				Protocol:   corev1.ProtocolTCP,
			}},
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
