// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestGatewayClassConfigDeepCopy(t *testing.T) {
	var nilConfig *GatewayClassConfig
	require.Nil(t, nilConfig.DeepCopy())
	require.Nil(t, nilConfig.DeepCopyObject())
	lbType := core.ServiceTypeLoadBalancer
	spec := GatewayClassConfigSpec{
		ServiceType: &lbType,
		NodeSelector: map[string]string{
			"test": "test",
		},
	}
	config := &GatewayClassConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: spec,
	}
	copy := config.DeepCopy()
	copyObject := config.DeepCopyObject()
	require.Equal(t, copy, copyObject)

	var nilSpec *GatewayClassConfigSpec
	require.Nil(t, nilSpec.DeepCopy())
	specCopy := (&spec).DeepCopy()
	require.Equal(t, spec.NodeSelector, specCopy.NodeSelector)

	var nilConfigList *GatewayClassConfigList
	require.Nil(t, nilConfigList.DeepCopyObject())
	configList := &GatewayClassConfigList{
		Items: []GatewayClassConfig{*config},
	}
	copyConfigList := configList.DeepCopy()
	copyConfigListObject := configList.DeepCopyObject()
	require.Equal(t, copyConfigList, copyConfigListObject)
}

func TestGatewayClassConfig_RoleFor(t *testing.T) {
	t.Run("managed auth with podSecurityPolicy", func(t *testing.T) {
		gcc := &GatewayClassConfig{}

		gw := &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: t.Name(), Namespace: t.Name()}}

		role := gcc.RoleFor(gw)
		require.NotNil(t, role)
		assert.Equal(t, t.Name(), role.Name)
		assert.Equal(t, t.Name(), role.Namespace)
		assert.Equal(t, labelsForGateway(gw), role.Labels)

		require.Len(t, role.Rules, 1)
		assert.ElementsMatch(t, []string{"policy"}, role.Rules[0].APIGroups)
		assert.ElementsMatch(t, []string{"podsecuritypolicies"}, role.Rules[0].Resources)
		assert.ElementsMatch(t, []string{"use"}, role.Rules[0].Verbs)
	})
}

func TestGatewayClassConfig_RoleBindingFor(t *testing.T) {
	t.Run("managed auth with podSecurityPolicy", func(t *testing.T) {
		gcc := &GatewayClassConfig{
			Spec: GatewayClassConfigSpec{},
		}

		gw := &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: t.Name(), Namespace: t.Name()}}

		binding := gcc.RoleBindingFor(gw)
		require.NotNil(t, binding)
		assert.Equal(t, gw.Name, binding.Name)
		assert.Equal(t, gw.Namespace, binding.Namespace)
		assert.Equal(t, labelsForGateway(gw), binding.Labels)

		assert.Equal(t, "rbac.authorization.k8s.io", binding.RoleRef.APIGroup)
		assert.Equal(t, "Role", binding.RoleRef.Kind)
		assert.Equal(t, gw.Name, binding.RoleRef.Name)

		require.Len(t, binding.Subjects, 1)
		assert.Equal(t, "ServiceAccount", binding.Subjects[0].Kind)
		assert.Equal(t, gw.Name, binding.Subjects[0].Name)
		assert.Equal(t, gw.Namespace, binding.Subjects[0].Namespace)
	})
}

func TestGatewayClassConfig_ServiceAccountFor(t *testing.T) {
	t.Run("managed auth", func(t *testing.T) {
		gcc := &GatewayClassConfig{}

		gw := &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: t.Name(), Namespace: t.Name()}}

		sa := gcc.ServiceAccountFor(gw)
		require.NotNil(t, sa)
		assert.Equal(t, t.Name(), sa.Name)
		assert.Equal(t, t.Name(), sa.Namespace)
		assert.Equal(t, labelsForGateway(gw), sa.Labels)
	})
}
