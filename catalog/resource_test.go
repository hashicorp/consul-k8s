package catalog

import (
	"testing"

	"github.com/hashicorp/consul-k8s/helper/controller"
)

func TestServiceResource_impl(t *testing.T) {
	var _ controller.Resource = &ServiceResource{}
	var _ controller.Backgrounder = &ServiceResource{}
}
