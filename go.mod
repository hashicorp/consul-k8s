module github.com/hashicorp/consul-k8s

require (
	github.com/cenkalti/backoff v2.1.1+incompatible
	github.com/deckarep/golang-set v1.7.1
	github.com/digitalocean/godo v1.10.0 // indirect
	github.com/fatih/color v1.10.0 // indirect
	github.com/go-logr/logr v0.4.0
	github.com/google/go-cmp v0.5.7
	github.com/google/go-querystring v1.0.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/hashicorp/consul/api v1.10.1-0.20221005170644-13da2c5fad69
	github.com/hashicorp/consul/sdk v0.11.0
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-discover v0.0.0-20200812215701-c4b85f6ed31f
	github.com/hashicorp/go-hclog v0.15.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/hashicorp/serf v0.10.1
	github.com/joyent/triton-go v1.7.1-0.20200416154420-6801d15b779f // indirect
	github.com/kr/text v0.2.0
	github.com/mattbaird/jsonpatch v0.0.0-20171005235357-81af80346b1a
	github.com/miekg/dns v1.1.41
	github.com/mitchellh/cli v1.1.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/radovskyb/watcher v1.0.2
	github.com/stretchr/testify v1.7.0
	go.uber.org/zap v1.19.0
	golang.org/x/net v0.0.0-20211216030914-fe4d6282115f
	gomodules.xyz/jsonpatch/v2 v2.2.0
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/klog/v2 v2.9.0
	sigs.k8s.io/controller-runtime v0.10.2
)

go 1.14
