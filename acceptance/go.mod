module github.com/hashicorp/consul-k8s/acceptance

go 1.14

require (
	github.com/gruntwork-io/terratest v0.31.2
	github.com/hashicorp/consul-k8s/control-plane v0.0.0-20211118191758-929940b5ab51
	github.com/hashicorp/consul/api v1.10.1-0.20211116182834-e6956893fb6f
	github.com/hashicorp/consul/sdk v0.8.0
	github.com/hashicorp/vault/api v1.2.0
	github.com/stretchr/testify v1.7.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
)
