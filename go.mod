module github.com/hashicorp/consul-k8s

require (
	github.com/cenkalti/backoff v2.1.1+incompatible
	github.com/deckarep/golang-set v1.7.1
	github.com/digitalocean/godo v1.10.0 // indirect
	github.com/hashicorp/consul v1.8.3
	github.com/hashicorp/consul/api v1.7.0
	github.com/hashicorp/consul/sdk v0.6.0
	github.com/hashicorp/go-discover v0.0.0-20200812215701-c4b85f6ed31f
	github.com/hashicorp/go-hclog v0.12.0
	github.com/hashicorp/go-msgpack v0.5.5 // indirect
	github.com/hashicorp/go-multierror v1.1.0
	github.com/hashicorp/go-sockaddr v1.0.2 // indirect
	github.com/imdario/mergo v0.3.8 // indirect
	github.com/kr/text v0.1.0
	github.com/mattbaird/jsonpatch v0.0.0-20171005235357-81af80346b1a
	github.com/mitchellh/cli v1.1.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/onsi/gomega v1.8.1 // indirect
	github.com/radovskyb/watcher v1.0.2
	github.com/stretchr/testify v1.5.1
	golang.org/x/net v0.0.0-20200625001655-4c5254603344 // indirect
	golang.org/x/oauth2 v0.0.0-20191202225959-858c2ad4c8b6 // indirect
	golang.org/x/sync v0.0.0-20200625203802-6e8e738ad208 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v0.18.2
)

replace github.com/hashicorp/consul => /Users/derekstrickland/code/consul-dev/consul

go 1.14
