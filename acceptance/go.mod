module github.com/hashicorp/consul-k8s/acceptance

go 1.14

require (
	github.com/gruntwork-io/terratest v0.31.2
	github.com/hashicorp/consul/api v1.10.1-0.20211101164201-d47b7311b8bb
	github.com/hashicorp/consul/sdk v0.8.0
	github.com/stretchr/testify v1.5.1
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.19.3
	k8s.io/apimachinery v0.19.3
	k8s.io/client-go v0.19.3
)
