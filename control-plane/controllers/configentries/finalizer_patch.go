// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"encoding/json"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type FinalizerPatcher struct{}

type FinalizerPatch struct {
	NewFinalizers []string
}

// Type implements client.Patch. Since this patch is used for a custom CRD, Kubernetes does not allow the more advanced
// StrategicMergePatch. Therefore, this patcher will replace the entire list of finalizers with the new list, rather
// than adding/removing individual entries.
//
// This can result in a small race condition where we could overwrite recently modified finalizers (either modified by a
// user or another controller process). Before the addition of this finalizer patcher implementation, this race
// condition still existed, but applied to the entirety of the CRD because we used to update the entire CRD rather than
// just the finalizer, so this reduces the surface area of the race condition. Generally we should not expect users or
// other configentries to be touching the finalizers of consul-k8s managed CRDs.
func (fp *FinalizerPatch) Type() types.PatchType {
	return types.MergePatchType
}

var _ client.Patch = (*FinalizerPatch)(nil)

func (f *FinalizerPatcher) AddFinalizersPatch(oldObj client.Object, addFinalizers ...string) *FinalizerPatch {
	output := make([]string, 0, len(addFinalizers))
	existing := make(map[string]bool)
	for _, f := range oldObj.GetFinalizers() {
		existing[f] = true
		output = append(output, f)
	}
	for _, f := range addFinalizers {
		if !existing[f] {
			output = append(output, f)
		}
	}
	return &FinalizerPatch{
		NewFinalizers: output,
	}
}

func (f *FinalizerPatcher) RemoveFinalizersPatch(oldObj client.Object, removeFinalizers ...string) *FinalizerPatch {
	output := make([]string, 0)
	remove := make(map[string]bool)
	for _, f := range removeFinalizers {
		remove[f] = true
	}
	for _, f := range oldObj.GetFinalizers() {
		if !remove[f] {
			output = append(output, f)
		}
	}
	return &FinalizerPatch{
		NewFinalizers: output,
	}
}

// Data implements client.Patch.
func (fp *FinalizerPatch) Data(obj client.Object) ([]byte, error) {
	newData, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"finalizers": fp.NewFinalizers,
		},
	})
	if err != nil {
		return nil, err
	}

	p, err := jsonpatch.CreateMergePatch([]byte(`{}`), newData)
	return p, err
}
