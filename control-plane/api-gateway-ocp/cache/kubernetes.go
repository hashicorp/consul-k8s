// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// configEntryObject is used for generic k8s events so we maintain the consul name/namespace.
type configEntryObject struct {
	client.Object // embed so we fufill the object interface

	Namespace string
	Name      string
}

func (c *configEntryObject) GetNamespace() string {
	return c.Namespace
}

func (c *configEntryObject) GetName() string {
	return c.Name
}

func newConfigEntryObject(namespacedName types.NamespacedName) *configEntryObject {
	return &configEntryObject{
		Namespace: namespacedName.Namespace,
		Name:      namespacedName.Name,
	}
}
