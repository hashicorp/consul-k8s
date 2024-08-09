// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package registration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"text/template"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	capi "github.com/hashicorp/consul/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const NotInServiceMeshFilter = "ServiceMeta[\"managed-by\"] != \"consul-k8s-endpoints-controller\""

func init() {
	gatewayTpl = template.Must(template.New("root").Parse(strings.TrimSpace(gatewayRulesTpl)))
}

type templateArgs struct {
	EnablePartitions bool
	Partition        string
	EnableNamespaces bool
	Namespace        string
	ServiceName      string
}

var (
	gatewayTpl      *template.Template
	gatewayRulesTpl = `
{{ if .EnablePartitions }}
partition "{{.Partition}}" {
{{- end }}
  {{- if .EnableNamespaces }}
  namespace "{{.Namespace}}" {
  {{- end }}
    service "{{.ServiceName}}" { 
      policy = "write" 
    }
  {{- if .EnableNamespaces }}
  }
  {{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`
)

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

func (c *RegistrationCache) aclsEnabled() bool {
	return c.ConsulClientConfig.APIClientConfig.Token != "" || c.ConsulClientConfig.APIClientConfig.TokenFile != ""
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

func (c *RegistrationCache) updateTermGWACLRole(log logr.Logger, registration *v1alpha1.Registration, termGWsToUpdate []v1alpha1.TerminatingGateway) error {
	client, err := consul.NewClientFromConnMgr(c.ConsulClientConfig, c.ConsulServerConnMgr)
	if err != nil {
		return err
	}

	var data bytes.Buffer
	if err := gatewayTpl.Execute(&data, templateArgs{
		EnablePartitions: c.partitionsEnabled,
		Partition:        defaultIfEmpty(registration.Spec.Service.Partition),
		EnableNamespaces: c.namespacesEnabled,
		Namespace:        defaultIfEmpty(registration.Spec.Service.Namespace),
		ServiceName:      registration.Spec.Service.Name,
	}); err != nil {
		// just panic if we can't compile the simple template
		// as it means something else is going severly wrong.
		panic(err)
	}

	existingPolicy, _, err := client.ACL().PolicyReadByName(servicePolicyName(registration.Spec.Service.Name), &capi.QueryOptions{})
	if err != nil {
		log.Error(err, "error reading policy")
		return err
	}

	if existingPolicy == nil {
		_, _, err = client.ACL().PolicyCreate(&capi.ACLPolicy{
			Name:        servicePolicyName(registration.Spec.Service.Name),
			Rules:       data.String(),
			Datacenters: []string{registration.Spec.Datacenter},
		}, nil)
		if err != nil {
			return err
		}
	}

	roles, _, err := client.ACL().RoleList(nil)
	if err != nil {
		return err
	}

	// for _, termGW := range termGWsToUpdate {
	termGWName := "terminating-gateway"
	terminatingGatewayRoleID := ""
	fmt.Println()
	for _, role := range roles {
		fmt.Println("RoleName: ", role.Name, "\nTermGWName: ", termGWName)
		fmt.Println()
		if strings.Contains(role.Name, termGWName) {
			// if strings.HasSuffix(role.Name, "terminating-gateway-acl-role") {
			terminatingGatewayRoleID = role.ID
			break
			// }
		}
	}

	log.Info("checking role: ", "terminatingGatewayRoleID: ", terminatingGatewayRoleID)
	if terminatingGatewayRoleID == "" {
		return errors.New("terminating gateway role not found")
	}

	termGwRole, _, err := client.ACL().RoleRead(terminatingGatewayRoleID, nil)
	if err != nil {
		return err
	}

	log.Info("checking read role:", "termGWRole: ", *termGwRole)

	for _, policy := range termGwRole.Policies {
		if policy.Name == servicePolicyName(registration.Spec.Service.Name) {
			log.Info("policy already exists on role", "policyName", policy.Name, "roleName", termGwRole.Name)
			return nil
		}
	}

	termGwRole.Policies = append(termGwRole.Policies, &capi.ACLRolePolicyLink{Name: servicePolicyName(registration.Spec.Service.Name)})
	role, _, err := client.ACL().RoleUpdate(termGwRole, nil)
	if err != nil {
		return err
	}

	if role.ID != terminatingGatewayRoleID {
		return errors.New("role ID mismatch")
	}
	// }

	log.Info("finished updating acl roles")
	return nil

	if existingPolicy == nil {
		_, _, err = client.ACL().PolicyCreate(&capi.ACLPolicy{
			Name:        servicePolicyName(registration.Spec.Service.Name),
			Description: "Write policy for terminating gateways for external service",
			Rules:       data.String(),
			Datacenters: []string{registration.Spec.Datacenter},
		}, nil)
		if err != nil {
			return err
		}
	}

	if len(termGWsToUpdate) == 0 {
		log.Info("terminating gateway not found")
		return errors.New("no terminating gateways found")
	}

	// the terminating gateway role is _always_ in the default namespace
	roles, _, err = client.ACL().RoleList(&capi.QueryOptions{})
	if err != nil {
		log.Error(err, "error reading role list")
		return err
	}

	var mErr error
	for _, termGW := range termGWsToUpdate {
		roleID := ""
		for _, r := range roles {
			if strings.HasSuffix(r.Name, fmt.Sprintf("%s-acl-role", termGW.Name)) {
				roleID = r.ID
				break
			}
		}

		if roleID == "" {
			log.Info("terminating gateway role not found", "terminatingGatewayName", termGW.Name)
			mErr = errors.Join(mErr, fmt.Errorf("terminating gateway role not found for %q", termGW.Name))
			continue
		}

		termGWRole, _, err := client.ACL().RoleRead(roleID, nil)
		if err != nil {
			mErr = errors.Join(mErr, fmt.Errorf("terminating gateway role failed to be read %q: %w", termGW.Name, err))
			continue
		}

		if termGWRole == nil {
			log.Info("terminating gateway role not found", "terminatingGatewayName", termGW.Name)
			mErr = errors.Join(mErr, fmt.Errorf("terminating gateway role not found for %q", termGW.Name))
			continue
		}

		termGWRole.Policies = append(termGWRole.Policies, &capi.ACLTokenPolicyLink{Name: servicePolicyName(registration.Spec.Service.Name)})

		log.Info("update role with policy")
		_, _, err = client.ACL().RoleUpdate(termGWRole, nil)
		if err != nil {
			log.Error(err, "error updating role", "roleName", termGWRole.Name)
			mErr = errors.Join(mErr, fmt.Errorf("error updating role %q", termGWRole.Name))
			continue
		}
	}

	log.Info("finished updating terminating gateway ACL role")
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
		log.Info("terminating gateway not found, no roles to remove")
		return nil
	}

	client, err := consul.NewClientFromConnMgr(c.ConsulClientConfig, c.ConsulServerConnMgr)
	if err != nil {
		return err
	}

	var mErr error
	for _, termGW := range termGWsToUpdate {

		// we don't need to include the namespace/partition here because all roles and policies are created in the default namespace for consul-k8s managed resources.
		queryOpts := &capi.QueryOptions{}
		writeOpts := &capi.WriteOptions{}

		roles, _, err := client.ACL().RoleList(queryOpts)
		if err != nil {
			return err
		}
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

		_, _, err = client.ACL().RoleUpdate(role, writeOpts)
		if err != nil {
			log.Error(err, "error updating role", "roleName", role.Name)
			mErr = errors.Join(mErr, fmt.Errorf("error updating role %q", role.Name))
			continue
		}

		_, err = client.ACL().PolicyDelete(policyID, writeOpts)
		if err != nil {
			log.Error(err, "error deleting service policy", "policyID", policyID, "policyName", expectedPolicyName)
			mErr = errors.Join(mErr, fmt.Errorf("error deleting service ACL policy %q", policyID))
			continue
		}
	}

	return mErr
}

func emptyOrDefault(s string) bool {
	return s == "" || s == "default"
}

func defaultIfEmpty(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

func servicePolicyName(name string) string {
	return fmt.Sprintf("%s-write-policy", name)
}
