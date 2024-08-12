// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package registration

import (
	"context"
	"strings"
	"sync"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	capi "github.com/hashicorp/consul/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const NotInServiceMeshFilter = "ServiceMeta[\"managed-by\"] != \"consul-k8s-endpoints-controller\""

type RegistrationCache struct {
	// we include the context here so that we can use it for cancellation of `run` invocations that are scheduled after the cache is started
	// this occurs when registering services in a new namespace as we have an invocation of `run` per namespace that is registered
	ctx context.Context

	ConsulClientConfig  *consul.Config
	ConsulServerConnMgr consul.ServerConnectionManager
	k8sClient           client.Client

	serviceMtx *sync.Mutex
	Services   map[string]*v1alpha1.Registration

	namespaces mapset.Set[string]

	synced     chan struct{}
	UpdateChan chan string

	namespacesEnabled bool
	partitionsEnabled bool
}

func NewRegistrationCache(ctx context.Context, consulClientConfig *consul.Config, consulServerConnMgr consul.ServerConnectionManager, k8sClient client.Client, namespacesEnabled, partitionsEnabled bool) *RegistrationCache {
	return &RegistrationCache{
		ctx:                 ctx,
		ConsulClientConfig:  consulClientConfig,
		ConsulServerConnMgr: consulServerConnMgr,
		k8sClient:           k8sClient,
		serviceMtx:          &sync.Mutex{},
		Services:            make(map[string]*v1alpha1.Registration),
		UpdateChan:          make(chan string),
		synced:              make(chan struct{}),
		namespaces:          mapset.NewSet[string](),
		namespacesEnabled:   namespacesEnabled,
		partitionsEnabled:   partitionsEnabled,
	}
}

// waitSynced is used to coordinate with the caller when the cache is initially filled.
func (c *RegistrationCache) waitSynced(ctx context.Context) {
	select {
	case <-c.synced:
		return
	case <-ctx.Done():
		return
	}
}

func (c *RegistrationCache) run(log logr.Logger, namespace string) {
	once := &sync.Once{}
	opts := &capi.QueryOptions{Filter: NotInServiceMeshFilter, Namespace: namespace}

	for {
		select {
		case <-c.ctx.Done():
			return
		default:

			consulClient, err := consul.NewClientFromConnMgr(c.ConsulClientConfig, c.ConsulServerConnMgr)
			if err != nil {
				log.Error(err, "error initializing consul client")
				continue
			}
			entries, meta, err := consulClient.Catalog().Services(opts.WithContext(c.ctx))
			if err != nil {
				// if we timeout we don't care about the error message because it's expected to happen on long polls
				// any other error we want to alert on
				if !strings.Contains(strings.ToLower(err.Error()), "timeout") &&
					!strings.Contains(strings.ToLower(err.Error()), "no such host") &&
					!strings.Contains(strings.ToLower(err.Error()), "connection refused") {
					log.Error(err, "error fetching registrations")
				}
				continue
			}

			servicesToRemove := mapset.NewSet[string]()
			servicesToAdd := mapset.NewSet[string]()
			c.serviceMtx.Lock()

			// Remove any services in the cache that are no longer in consul
			for svc := range c.Services {
				if _, ok := entries[svc]; !ok {
					servicesToRemove.Add(svc)
				}
			}

			// Add any services to the cache that are in consul but not in the cache (we expect to hit this loop on a reboot)
			for svc := range entries {
				if _, ok := c.Services[svc]; !ok && svc != "consul" {
					servicesToAdd.Add(svc)
				}
			}
			c.serviceMtx.Unlock()

			for _, svc := range servicesToRemove.ToSlice() {
				log.Info("consul deregistered service", "svcName", svc)
				c.UpdateChan <- svc
			}

			for _, svc := range servicesToAdd.ToSlice() {
				log.Info("consul registered service", "svcName", svc)
				registrationList := &v1alpha1.RegistrationList{}

				if err := c.k8sClient.List(context.Background(), registrationList, client.MatchingFields{registrationByServiceNameIndex: svc}); err != nil {
					log.Error(err, "error listing registrations", "svcName", svc)
				}

				found := false
				for _, reg := range registrationList.Items {
					if reg.Spec.Service.Name == svc {
						found = true
						c.set(svc, &reg)
					}
				}

				if !found {
					log.Info("registration not found in k8s", "svcName", svc)
				}
			}

			log.Info("synced registrations with consul")

			opts.WaitIndex = meta.LastIndex
			once.Do(func() {
				log.Info("Initial sync complete")
				c.synced <- struct{}{}
			})
		}
	}
}

func (c *RegistrationCache) get(svcName string) (*v1alpha1.Registration, bool) {
	c.serviceMtx.Lock()
	defer c.serviceMtx.Unlock()
	val, ok := c.Services[svcName]
	return val, ok
}

func (c *RegistrationCache) set(name string, reg *v1alpha1.Registration) {
	c.serviceMtx.Lock()
	defer c.serviceMtx.Unlock()
	c.Services[name] = reg
}

func (c *RegistrationCache) registerService(log logr.Logger, reg *v1alpha1.Registration) error {
	if svc, ok := c.get(reg.Spec.Service.Name); ok {
		if reg.EqualExceptStatus(svc) {
			log.Info("service already registered", "svcName", reg.Spec.Service.Name)
			return nil
		}
	}

	client, err := consul.NewClientFromConnMgr(c.ConsulClientConfig, c.ConsulServerConnMgr)
	if err != nil {
		return err
	}

	regReq, err := reg.ToCatalogRegistration()
	if err != nil {
		return err
	}

	_, err = client.Catalog().Register(regReq, &capi.WriteOptions{Namespace: reg.Spec.Service.Namespace})
	if err != nil {
		log.Error(err, "error registering service", "svcName", regReq.Service.Service)
		return err
	}

	if !c.namespaces.Contains(reg.Spec.Service.Namespace) && !emptyOrDefault(reg.Spec.Service.Namespace) {
		c.namespaces.Add(reg.Spec.Service.Namespace)
		go c.run(log, reg.Spec.Service.Namespace)
	}

	log.Info("Successfully registered service", "svcName", regReq.Service.Service)

	return nil
}

func (c *RegistrationCache) deregisterService(log logr.Logger, reg *v1alpha1.Registration) error {
	client, err := consul.NewClientFromConnMgr(c.ConsulClientConfig, c.ConsulServerConnMgr)
	if err != nil {
		return err
	}

	deRegReq := reg.ToCatalogDeregistration()
	_, err = client.Catalog().Deregister(deRegReq, nil)
	if err != nil {
		log.Error(err, "error deregistering service", "svcID", deRegReq.ServiceID)
		return err
	}

	c.serviceMtx.Lock()
	defer c.serviceMtx.Unlock()
	delete(c.Services, reg.Spec.Service.Name)

	log.Info("Successfully deregistered service", "svcID", deRegReq.ServiceID)
	return nil
}

func emptyOrDefault(s string) bool {
	return s == "" || s == "default"
}
