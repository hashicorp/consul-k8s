package registration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	capi "github.com/hashicorp/consul/api"
)

const NotInServiceMeshFilter = "ServiceMeta[\"managed-by\"] != \"consul-k8s-endpoints-controller\""

type RegistrationCache struct {
	ConsulClientConfig  *consul.Config
	ConsulServerConnMgr consul.ServerConnectionManager
	serviceMtx          *sync.Mutex
	Services            map[string]*v1alpha1.Registration
	synced              chan struct{}
	UpdateChan          chan string
}

func NewRegistrationCache(consulClientConfig *consul.Config, consulServerConnMgr consul.ServerConnectionManager) *RegistrationCache {
	return &RegistrationCache{
		ConsulClientConfig:  consulClientConfig,
		ConsulServerConnMgr: consulServerConnMgr,
		serviceMtx:          &sync.Mutex{},
		Services:            make(map[string]*v1alpha1.Registration),
		UpdateChan:          make(chan string),
		synced:              make(chan struct{}),
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

func (c *RegistrationCache) run(ctx context.Context, log logr.Logger) {
	once := &sync.Once{}
	opts := &capi.QueryOptions{Filter: NotInServiceMeshFilter}

	for {
		select {
		case <-ctx.Done():
			return
		default:

			client, err := consul.NewClientFromConnMgr(c.ConsulClientConfig, c.ConsulServerConnMgr)
			if err != nil {
				log.Error(err, "error initializing consul client")
				continue
			}
			entries, meta, err := client.Catalog().Services(opts.WithContext(ctx))
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

			diffs := mapset.NewSet[string]()
			c.serviceMtx.Lock()
			for svc := range c.Services {
				if _, ok := entries[svc]; !ok {
					diffs.Add(svc)
				}
			}
			c.serviceMtx.Unlock()

			for _, svc := range diffs.ToSlice() {
				log.Info("consul deregistered service", "svcName", svc)
				c.UpdateChan <- svc
			}

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

func (c *RegistrationCache) aclsEnabled() bool {
	return c.ConsulClientConfig.APIClientConfig.Token != "" || c.ConsulClientConfig.APIClientConfig.TokenFile != ""
}

func (c *RegistrationCache) registerService(log logr.Logger, reg *v1alpha1.Registration) error {
	client, err := consul.NewClientFromConnMgr(c.ConsulClientConfig, c.ConsulServerConnMgr)
	if err != nil {
		return err
	}

	regReq, err := reg.ToCatalogRegistration()
	if err != nil {
		return err
	}

	_, err = client.Catalog().Register(regReq, nil)
	if err != nil {
		log.Error(err, "error registering service", "svcName", regReq.Service.Service)
		return err
	}

	c.serviceMtx.Lock()
	defer c.serviceMtx.Unlock()
	c.Services[reg.Spec.Service.Name] = reg

	log.Info("Successfully registered service", "svcName", regReq.Service.Service)

	return nil
}

func (c *RegistrationCache) updateTermGWACLRole(log logr.Logger, registration *v1alpha1.Registration, termGWsToUpdate []v1alpha1.TerminatingGateway) error {
	if len(termGWsToUpdate) == 0 {
		log.Info("terminating gateway not found")
		return nil
	}

	client, err := consul.NewClientFromConnMgr(c.ConsulClientConfig, c.ConsulServerConnMgr)
	if err != nil {
		return err
	}

	roles, _, err := client.ACL().RoleList(nil)
	if err != nil {
		log.Error(err, "error reading role list")
		return err
	}

	policy := &capi.ACLPolicy{
		Name:        servicePolicyName(registration.Spec.Service.Name),
		Description: "Write policy for terminating gateways for external service",
		Rules:       fmt.Sprintf(`service %q { policy = "write" }`, registration.Spec.Service.Name),
		Datacenters: []string{registration.Spec.Datacenter},
		Namespace:   registration.Spec.Service.Namespace,
		Partition:   registration.Spec.Service.Partition,
	}

	existingPolicy, _, err := client.ACL().PolicyReadByName(policy.Name, nil)
	if err != nil {
		log.Error(err, "error reading policy")
		return err
	}

	if existingPolicy == nil {
		policy, _, err = client.ACL().PolicyCreate(policy, nil)
		if err != nil {
			return fmt.Errorf("error creating policy: %w", err)
		}
	} else {
		policy = existingPolicy
	}

	var mErr error
	for _, termGW := range termGWsToUpdate {
		var role *capi.ACLRole
		for _, r := range roles {
			if strings.HasSuffix(r.Name, fmt.Sprintf("-%s-acl-role", termGW.Name)) {
				role = r
				break
			}
		}

		if role == nil {
			log.Info("terminating gateway role not found", "terminatingGatewayName", termGW.Name)
			mErr = errors.Join(mErr, fmt.Errorf("terminating gateway role not found for %q", termGW.Name))
			continue
		}

		role.Policies = append(role.Policies, &capi.ACLRolePolicyLink{Name: policy.Name, ID: policy.ID})

		_, _, err = client.ACL().RoleUpdate(role, nil)
		if err != nil {
			log.Error(err, "error updating role", "roleName", role.Name)
			mErr = errors.Join(mErr, fmt.Errorf("error updating role %q", role.Name))
			continue
		}
	}

	return mErr
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

func (c *RegistrationCache) removeTermGWACLRole(log logr.Logger, registration *v1alpha1.Registration, termGWsToUpdate []v1alpha1.TerminatingGateway) error {
	if len(termGWsToUpdate) == 0 {
		log.Info("terminating gateway not found")
		return nil
	}

	client, err := consul.NewClientFromConnMgr(c.ConsulClientConfig, c.ConsulServerConnMgr)
	if err != nil {
		return err
	}

	roles, _, err := client.ACL().RoleList(nil)
	if err != nil {
		return err
	}

	var mErr error
	for _, termGW := range termGWsToUpdate {
		var role *capi.ACLRole
		for _, r := range roles {
			if strings.HasSuffix(r.Name, fmt.Sprintf("-%s-acl-role", termGW.Name)) {
				role = r
				break
			}
		}

		if role == nil {
			log.Info("terminating gateway role not found", "terminatingGatewayName", termGW.Name)
			mErr = errors.Join(mErr, fmt.Errorf("terminating gateway role not found for %q", termGW.Name))
			continue
		}

		var policyID string

		expectedPolicyName := servicePolicyName(registration.Spec.Service.Name)
		role.Policies = slices.DeleteFunc(role.Policies, func(i *capi.ACLRolePolicyLink) bool {
			if i.Name == expectedPolicyName {
				policyID = i.ID
				return true
			}
			return false
		})

		if policyID == "" {
			log.Info("policy not found on terminating gateway role", "policyName", expectedPolicyName, "terminatingGatewayName", termGW.Name)
			continue
		}

		_, _, err = client.ACL().RoleUpdate(role, nil)
		if err != nil {
			log.Error(err, "error updating role", "roleName", role.Name)
			mErr = errors.Join(mErr, fmt.Errorf("error updating role %q", role.Name))
			continue
		}

		_, err = client.ACL().PolicyDelete(policyID, nil)
		if err != nil {
			log.Error(err, "error deleting service policy", "policyID", policyID, "policyName", expectedPolicyName)
			mErr = errors.Join(mErr, fmt.Errorf("error deleting service ACL policy %q", policyID))
			continue
		}
	}

	return mErr
}

func servicePolicyName(name string) string {
	return fmt.Sprintf("%s-write-policy", name)
}
