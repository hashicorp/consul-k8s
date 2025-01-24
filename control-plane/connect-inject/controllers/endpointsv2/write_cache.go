// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpointsv2

import (
	"bytes"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	"sync"
)

// consulWriteRecord is a record of writing a resource to Consul for the sake of deduplicating writes.
//
// It is bounded in size and even a low-resource pod should be able to store 10Ks of them in-memory without worrying
// about eviction. On average, assuming a SHA256 hash, the total size of each record should be approximately 150 bytes.
type consulWriteRecord struct {
	// inputHash is a detrministic hash of the payload written to Consul.
	// It should be derived from the "source" data rather than the returned payload in order to be unaffected by added
	// fields and defaulting behavior defined by Consul.
	inputHash []byte
	// generation is the generation of the written resource in Consul. This ensures that we write to Consul if a
	// redundant reconcile occurs, but the actual Consul resource has been modified since the last write.
	generation string
	// k8sUid is the UID of the corresponding resource in K8s. This allows us to check for K8s service recreation in
	// between successful reconciles even though deletion of a K8s resource does not expose the UID of the deleted
	// resource (the reconcile request only contains the namespaced name).
	k8sUid string
}

// WriteCache is a simple, unbounded, thread-safe in-memory cache for tracking writes of Consul resources.
// It can be used to deduplicate identical writes client-side to "debounce" writes during repeat reconciles
// that do not impact data already written to Consul.
type WriteCache interface {
	hasMatch(key string, hash []byte, generationFetchFn func() string, k8sUid string) bool
	update(key string, hash []byte, generation string, k8sUid string)
	remove(key string)
}

type writeCache struct {
	data      map[string]consulWriteRecord
	dataMutex sync.RWMutex

	log logr.Logger
}

func NewWriteCache(log logr.Logger) WriteCache {
	return &writeCache{
		data: make(map[string]consulWriteRecord),
		log:  log.WithName("writeCache"),
	}
}

// update upserts a record containing the given hash and generation to the cache at the given key.
func (c *writeCache) update(key string, hash []byte, generation string, k8sUid string) {
	c.dataMutex.Lock()
	defer c.dataMutex.Unlock()

	var err error
	if key == "" {
		err = multierror.Append(err, fmt.Errorf("key was empty"))
	}
	if len(hash) == 0 {
		err = multierror.Append(err, fmt.Errorf("hash was empty"))
	}
	if generation == "" {
		err = multierror.Append(err, fmt.Errorf("generation was empty"))
	}
	if k8sUid == "" {
		err = multierror.Append(err, fmt.Errorf("k8sUid was empty"))
	}
	if err != nil {
		c.log.Error(err, "writeCache could not be updated due to empty value(s) - redundant writes may be repeated")
		return
	}

	c.data[key] = consulWriteRecord{
		inputHash:  hash,
		generation: generation,
		k8sUid:     k8sUid,
	}
}

// remove removes a record from the cache at the given key.
func (c *writeCache) remove(key string) {
	c.dataMutex.Lock()
	defer c.dataMutex.Unlock()

	delete(c.data, key)
}

// hasMatch returns true iff. there is an existing write record for the given key in the cache, and that record matches
// the provided non-empty hash, generation, and Kubernetes UID.
//
// The generation is fetched rather than provided directly s.t. a call to Consul can be skipped if a record is not found
// or other available fields do not match.
//
// While not strictly necessary assuming the controller is the sole writer of the resource, the generation check ensures
// that the resource is kept in sync even if externally modified.
//
// When checking for a match, ensures the UID of the K8s service also matches s.t. we don't skip updates on recreation
// of a K8s service, as the intent of the user may have been to force a sync, and a future solution that stores write
// fingerprints in K8s annotations would also have this behavior.
func (c *writeCache) hasMatch(key string, hash []byte, generationFetchFn func() string, k8sUid string) bool {
	var lastHash []byte
	lastGeneration := ""
	lastK8sUid := ""
	if s, ok := c.get(key); ok {
		lastHash = s.inputHash
		lastGeneration = s.generation
		lastK8sUid = s.k8sUid
	}

	if len(lastHash) == 0 || lastGeneration == "" || lastK8sUid == "" {
		return false
	}

	return bytes.Equal(lastHash, hash) &&
		lastK8sUid == k8sUid &&
		lastGeneration == generationFetchFn() // Fetch generation only if other fields match
}

func (c *writeCache) get(key string) (consulWriteRecord, bool) {
	c.dataMutex.RLock()
	defer c.dataMutex.RUnlock()

	v, ok := c.data[key]
	return v, ok
}
