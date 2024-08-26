// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

const (
	oldACLRoleName   = "managed-gateway-acl-role"
	oldACLPolicyName = "api-gateway-token-policy"
)

var sleepTime = 10 * time.Minute

type Cleaner struct {
	Logger       logr.Logger
	ConsulConfig *consul.Config
	ServerMgr    consul.ServerConnectionManager
	AuthMethod   string
}

// Run periodically cleans up old ACL roles and policies as well as orphaned inline certificate config entries.
// When it detects that there are no more inline-certificates and that the old ACL role and policy are not in use, it exits.
func (c Cleaner) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleepTime):
		}

		client, err := consul.NewClientFromConnMgr(c.ConsulConfig, c.ServerMgr)
		if err != nil {
			c.Logger.Error(err, "failed to create Consul client")
			continue
		}

		aclsCleanedUp, err := c.cleanupACLRoleAndPolicy(client)
		if err != nil {
			c.Logger.Error(err, "failed to cleanup old ACL role and policy")
		}

		if aclsCleanedUp {
			c.Logger.Info("Cleanup complete")
			return
		}
	}
}

// cleanupACLRoleAndPolicy deletes the old shared gateway ACL role and policy if they exist.
func (c Cleaner) cleanupACLRoleAndPolicy(client *api.Client) (bool, error) {
	existingRules, _, err := client.ACL().BindingRuleList(c.AuthMethod, &api.QueryOptions{})
	if err != nil {
		if err.Error() == "Unexpected response code: 401 (ACL support disabled)" {
			return true, nil
		}
		return false, fmt.Errorf("failed to list binding rules: %w", err)
	}

	oldBindingRules := make(map[string]*api.ACLBindingRule)

	// here we need to find binding rules with the old name that have a matching selector to the new gateway specific binding rule
	// so we first get all the old rules and put them into a map and then ensure we can delete the old rule by finding the new rule that replaces it
	// by matching the selector
	for _, rule := range existingRules {
		if rule.BindName == oldACLRoleName {
			oldBindingRules[rule.Selector] = rule
		}
	}

	rulesToDelete := mapset.NewSet[string]()

	for _, rule := range existingRules {
		if ruleToDelete, ok := oldBindingRules[rule.Selector]; ok && rule.BindName != oldACLRoleName {
			rulesToDelete.Add(ruleToDelete.ID)
		}
	}

	var mErr error
	deletedRuleCount := 0
	for ruleID := range rulesToDelete.Iter() {
		_, err := client.ACL().BindingRuleDelete(ruleID, &api.WriteOptions{})
		if ignoreNotFoundError(err) != nil {
			mErr = errors.Join(mErr, fmt.Errorf("failed to delete binding rule: %w", err))
		} else {
			c.Logger.Info("Deleted unused binding rule", "id", ruleID)
			deletedRuleCount++
		}
	}

	if mErr != nil {
		return false, mErr
	}

	if deletedRuleCount != len(oldBindingRules) {
		return false, nil
	}

	role, _, err := client.ACL().RoleReadByName(oldACLRoleName, &api.QueryOptions{})
	if ignoreNotFoundError(err) != nil {
		return false, fmt.Errorf("failed to get role: %w", err)
	}

	if role != nil {
		_, err = client.ACL().RoleDelete(role.ID, &api.WriteOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to delete role: %w", err)
		}
		c.Logger.Info("Deleted unused ACL role", "id", role.ID)
	}

	policy, _, err := client.ACL().PolicyReadByName(oldACLPolicyName, &api.QueryOptions{})
	if ignoreNotFoundError(err) != nil {
		return false, fmt.Errorf("failed to get policy: %w", err)
	}

	if policy != nil {
		_, err = client.ACL().PolicyDelete(policy.ID, &api.WriteOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to delete policy: %w", err)
		}
		c.Logger.Info("Deleted unused ACL policy", "id", policy.ID)
	}

	return true, nil
}

func ignoreNotFoundError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "Unexpected response code: 404") {
		return nil
	}

	return err
}
