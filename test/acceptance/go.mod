module github.com/hashicorp/consul-helm/test/acceptance

go 1.14

require (
	github.com/gruntwork-io/terratest v0.27.3
	github.com/hashicorp/consul/api v1.5.0
	github.com/hashicorp/consul/sdk v0.5.0
	github.com/hashicorp/serf v0.9.0
	github.com/stretchr/testify v1.5.1
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.17.0
	k8s.io/apimachinery v0.17.0
	k8s.io/client-go v0.17.0
)
