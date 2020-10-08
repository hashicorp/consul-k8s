module github.com/hashicorp/consul-helm/test/acceptance

go 1.14

require (
	github.com/gruntwork-io/terratest v0.29.0
	github.com/hashicorp/consul/api v1.4.1-0.20201007080954-aa0f5ff839c5
	github.com/hashicorp/consul/sdk v0.6.0
	github.com/stretchr/testify v1.5.1
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.18.3
	k8s.io/apimachinery v0.18.3
	k8s.io/client-go v0.18.3
)
