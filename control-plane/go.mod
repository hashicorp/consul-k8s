module github.com/hashicorp/consul-k8s/control-plane

require (
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/containernetworking/cni v1.1.1
	github.com/deckarep/golang-set v1.7.1
	github.com/fsnotify/fsnotify v1.5.4
	github.com/go-logr/logr v0.4.0
	github.com/google/go-cmp v0.5.7
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/hashicorp/consul-k8s/control-plane/cni v0.0.0-20220831174802-b8af65262de8
	github.com/hashicorp/consul/api v1.10.1-0.20221005170644-13da2c5fad69
	github.com/hashicorp/consul/sdk v0.11.0
	github.com/hashicorp/go-discover v0.0.0-20200812215701-c4b85f6ed31f
	github.com/hashicorp/go-hclog v1.2.2
	github.com/hashicorp/go-multierror v1.1.1
	github.com/hashicorp/serf v0.10.1
	github.com/kr/text v0.2.0
	github.com/miekg/dns v1.1.41
	github.com/mitchellh/cli v1.1.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mitchellh/mapstructure v1.4.1
	github.com/stretchr/testify v1.7.2
	go.uber.org/zap v1.19.0
	golang.org/x/text v0.7.0
	golang.org/x/time v0.0.0-20220210224613-90d013bbcef8
	gomodules.xyz/jsonpatch/v2 v2.2.0
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/klog/v2 v2.9.0
	k8s.io/utils v0.0.0-20220812165043-ad590609e2e5
	sigs.k8s.io/controller-runtime v0.10.2
)

require (
	cloud.google.com/go v0.81.0 // indirect
	github.com/Azure/azure-sdk-for-go v44.0.0+incompatible // indirect
	github.com/Azure/go-autorest v14.2.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.18 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.13 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.0 // indirect
	github.com/Azure/go-autorest/autorest/azure/cli v0.4.0 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.0 // indirect
	github.com/Azure/go-autorest/autorest/to v0.4.0 // indirect
	github.com/Azure/go-autorest/autorest/validation v0.3.0 // indirect
	github.com/Azure/go-autorest/logger v0.2.1 // indirect
	github.com/Azure/go-autorest/tracing v0.6.0 // indirect
	github.com/armon/go-metrics v0.3.10 // indirect
	github.com/armon/go-radix v1.0.0 // indirect
	github.com/aws/aws-sdk-go v1.25.41 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bgentry/speakeasy v0.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/denverdino/aliyungo v0.0.0-20170926055100-d3308649c661 // indirect
	github.com/digitalocean/godo v1.10.0 // indirect
	github.com/dimchansky/utfbom v1.1.0 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/fatih/color v1.13.0 // indirect
	github.com/form3tech-oss/jwt-go v3.2.3+incompatible // indirect
	github.com/go-logr/zapr v0.4.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-querystring v1.0.0 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/google/uuid v1.1.2 // indirect
	github.com/googleapis/gax-go/v2 v2.0.5 // indirect
	github.com/googleapis/gnostic v0.5.5 // indirect
	github.com/gophercloud/gophercloud v0.1.0 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.0 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-uuid v1.0.2 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/hashicorp/mdns v1.0.4 // indirect
	github.com/hashicorp/vic v1.5.1-0.20190403131502-bbfe86ec9443 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/jmespath/go-jmespath v0.0.0-20180206201540-c2b33e8439af // indirect
	github.com/joyent/triton-go v1.7.1-0.20200416154420-6801d15b779f // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/linode/linodego v0.7.1 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/nicolai86/scaleway-sdk v1.10.2-0.20180628010248-798f60e20bb2 // indirect
	github.com/packethost/packngo v0.1.1-0.20180711074735-b9cb5096f54c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/posener/complete v1.2.3 // indirect
	github.com/prometheus/client_golang v1.11.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.26.0 // indirect
	github.com/prometheus/procfs v0.6.0 // indirect
	github.com/renier/xmlrpc v0.0.0-20170708154548-ce4a1a486c03 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/softlayer/softlayer-go v0.0.0-20180806151055-260589d94c7d // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stretchr/objx v0.2.0 // indirect
	github.com/tencentcloud/tencentcloud-sdk-go v3.0.83+incompatible // indirect
	github.com/vmware/govmomi v0.18.0 // indirect
	go.opencensus.io v0.23.0 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	golang.org/x/crypto v0.0.0-20220411220226-7b82a4e95df4 // indirect
	golang.org/x/net v0.7.0 // indirect
	golang.org/x/oauth2 v0.0.0-20211104180415-d3ed0bb246c8 // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c // indirect
	golang.org/x/sys v0.5.0 // indirect
	golang.org/x/term v0.5.0 // indirect
	golang.org/x/tools v0.2.0 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/api v0.43.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20210602131652-f16073e35f0c // indirect
	google.golang.org/grpc v1.48.0 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/resty.v1 v1.12.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.22.2 // indirect
	k8s.io/component-base v0.22.2 // indirect
	k8s.io/kube-openapi v0.0.0-20210421082810-95288971da7e // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.1.2 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)

go 1.19
