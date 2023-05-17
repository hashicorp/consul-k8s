package binding

import (
	"github.com/hashicorp/consul/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type KubernetesSnapshot struct {
	Updates       []client.Object
	StatusUpdates []client.Object
}

type ConsulSnapshot struct {
	Updates   []api.ConfigEntry
	Deletions []api.ResourceReference
}

type Snapshot struct {
	Kubernetes KubernetesSnapshot
	Consul     ConsulSnapshot
}
