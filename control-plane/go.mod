module github.com/hashicorp/consul-k8s/control-plane

require (
	github.com/armon/go-metrics v0.3.9 // indirect
	github.com/cenkalti/backoff v2.1.1+incompatible
	github.com/deckarep/golang-set v1.7.1
	github.com/digitalocean/godo v1.10.0 // indirect
	github.com/fatih/color v1.12.0 // indirect
	github.com/go-logr/logr v0.4.0
	github.com/google/go-cmp v0.5.6
	github.com/google/go-querystring v1.0.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/hashicorp/consul/api v1.10.1-0.20211101164201-d47b7311b8bb
	github.com/hashicorp/consul/sdk v0.8.0
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-discover v0.0.0-20200812215701-c4b85f6ed31f
	github.com/hashicorp/go-hclog v0.16.1
	github.com/hashicorp/go-immutable-radix v1.3.0 // indirect
	github.com/hashicorp/go-msgpack v0.5.5 // indirect
	github.com/hashicorp/go-multierror v1.1.0
	github.com/hashicorp/go-sockaddr v1.0.2 // indirect
	github.com/hashicorp/go-uuid v1.0.2 // indirect
	github.com/hashicorp/serf v0.9.5
	github.com/joyent/triton-go v1.7.1-0.20200416154420-6801d15b779f // indirect
	github.com/kr/text v0.2.0
	github.com/mattn/go-isatty v0.0.13 // indirect
	github.com/mitchellh/cli v1.1.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mitchellh/go-testing-interface v1.14.0 // indirect
	github.com/mitchellh/mapstructure v1.4.1
	github.com/stretchr/objx v0.2.0 // indirect
	github.com/stretchr/testify v1.7.0
	go.uber.org/zap v1.19.0
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac
	gomodules.xyz/jsonpatch/v2 v2.2.0
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/klog/v2 v2.9.0
	sigs.k8s.io/controller-runtime v0.10.2
)

go 1.16
