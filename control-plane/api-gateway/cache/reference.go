// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"sync"

	"github.com/hashicorp/consul/api"
)

// ReferenceMap is contains a map of config entries stored
// by their normalized resource references (with empty string
// for namespaces and partitions stored as "default")
type ReferenceMap struct {
	data  map[api.ResourceReference]api.ConfigEntry
	mutex sync.RWMutex
}

// NewReferenceMap constructs a reference map.
func NewReferenceMap() *ReferenceMap {
	return &ReferenceMap{
		data: make(map[api.ResourceReference]api.ConfigEntry),
	}
}

// Set adds an entry to the reference map.
func (r *ReferenceMap) Set(ref api.ResourceReference, v api.ConfigEntry) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.data[normalizeMeta(ref)] = v
}

// Get returns an entry from the reference map.
func (r *ReferenceMap) Get(ref api.ResourceReference) api.ConfigEntry {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	v, ok := r.data[normalizeMeta(ref)]
	if !ok {
		return nil
	}
	return v
}

// Entries returns a list of entries stored in the reference map.
func (r *ReferenceMap) Entries() []api.ConfigEntry {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	entries := make([]api.ConfigEntry, 0, len(r.data))
	for _, entry := range r.data {
		entries = append(entries, entry)
	}
	return entries
}

// Delete deletes an entry stored in the reference map.
func (r *ReferenceMap) Delete(ref api.ResourceReference) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	delete(r.data, normalizeMeta(ref))
}

// Diff calculates the difference between the stored entries in two reference maps.
func (r *ReferenceMap) Diff(other *ReferenceMap) []api.ConfigEntry {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	other.mutex.RLock()
	defer other.mutex.RUnlock()

	diffs := make([]api.ConfigEntry, 0)

	for ref, entry := range other.data {
		oldRef := r.Get(ref)
		// ref from the new cache doesn't exist in the old one
		// this means a resource was added
		if oldRef == nil {
			diffs = append(diffs, entry)
			continue
		}

		// the entry in the old cache has an older modify index than the ref
		// from the new cache
		if oldRef.GetModifyIndex() < entry.GetModifyIndex() {
			diffs = append(diffs, entry)
		}
	}

	// get all deleted entries, these are entries present in the old cache
	// that are not present in the new
	for ref, entry := range r.data {
		if other.Get(ref) == nil {
			diffs = append(diffs, entry)
		}
	}

	return diffs
}

// ReferenceSet is a set of stored references.
type ReferenceSet struct {
	data  map[api.ResourceReference]struct{}
	mutex sync.RWMutex
}

// NewReferenceSet constructs a new reference set.
func NewReferenceSet() *ReferenceSet {
	return &ReferenceSet{
		data: make(map[api.ResourceReference]struct{}),
	}
}

// Mark adds a reference to the reference set.
func (r *ReferenceSet) Mark(ref api.ResourceReference) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.data[normalizeMeta(ref)] = struct{}{}
}

// Contains checks for the inclusion of a reference in the set.
func (r *ReferenceSet) Contains(ref api.ResourceReference) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	_, ok := r.data[normalizeMeta(ref)]
	return ok
}

// Remove drops a reference from the set.
func (r *ReferenceSet) Remove(ref api.ResourceReference) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	delete(r.data, normalizeMeta(ref))
}

func normalizeMeta(ref api.ResourceReference) api.ResourceReference {
	ref.Namespace = normalizeEmptyMetadataString(ref.Namespace)
	ref.Partition = normalizeEmptyMetadataString(ref.Partition)
	return ref
}

func normalizeEmptyMetadataString(metaString string) string {
	if metaString == "" {
		return "default"
	}
	return metaString
}
