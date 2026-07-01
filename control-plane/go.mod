module github.com/hashicorp/consul-k8s/control-plane

go 1.26.0

replace github.com/hashicorp/consul-k8s/version => ../version

replace github.com/hashicorp/consul-k8s/control-plane/cni => ./cni

replace github.com/hashicorp/consul/api => github.com/hashicorp/consul/api v1.32.2-0.20260701104039-a0c11b9c505b

replace github.com/hashicorp/consul/sdk => github.com/hashicorp/consul/sdk v0.18.1-0.20260701104039-a0c11b9c505b

replace github.com/hashicorp/consul => github.com/hashicorp/consul v1.11.0-alpha.0.20260701104039-a0c11b9c505b

require (
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/containernetworking/cni v1.3.0
	github.com/deckarep/golang-set v1.8.0
	github.com/deckarep/golang-set/v2 v2.9.0
	github.com/evanphx/json-patch v5.9.11+incompatible
	github.com/fsnotify/fsnotify v1.10.1
	github.com/go-logr/logr v1.4.3
	github.com/google/go-cmp v0.7.0
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/hashicorp/consul v1.22.7
	github.com/hashicorp/consul-k8s/control-plane/cni v0.0.0-20260701055524-e5f3782bed9c
	github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom v0.1.6
	github.com/hashicorp/consul-k8s/version v0.0.0
	github.com/hashicorp/consul-server-connection-manager v0.1.12
	github.com/hashicorp/consul/api v1.34.3
	github.com/hashicorp/consul/sdk v0.18.1
	github.com/hashicorp/go-bexpr v0.1.16
	github.com/hashicorp/go-discover v1.3.0
	github.com/hashicorp/go-hclog v1.6.3
	github.com/hashicorp/go-metrics v0.6.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/hashicorp/go-netaddrs v0.1.0
	github.com/hashicorp/go-rootcerts v1.0.2
	github.com/hashicorp/go-uuid v1.0.3
	github.com/hashicorp/go-version v1.9.0
	github.com/hashicorp/serf v0.10.2
	github.com/hashicorp/vault/api v1.23.0
	github.com/kr/text v0.2.0
	github.com/miekg/dns v1.1.72
	github.com/mitchellh/cli v1.1.5
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mitchellh/mapstructure v1.5.0
	github.com/prometheus/client_golang v1.23.2
	github.com/stretchr/testify v1.11.1
	go.uber.org/zap v1.28.0
	golang.org/x/exp v0.0.0-20260611194520-c48552f49976
	golang.org/x/text v0.38.0
	golang.org/x/time v0.15.0
	gomodules.xyz/jsonpatch/v2 v2.5.0
	google.golang.org/grpc v1.82.0
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.36.2
	k8s.io/apiextensions-apiserver v0.36.2
	k8s.io/apimachinery v0.36.2
	k8s.io/client-go v0.36.2
	k8s.io/klog/v2 v2.140.0
	k8s.io/utils v0.0.0-20260626114624-be93311217bd
	sigs.k8s.io/controller-runtime v0.24.1
	sigs.k8s.io/gateway-api v1.6.0
)

require (
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	dario.cat/mergo v1.0.2 // indirect
	github.com/Azure/azure-sdk-for-go v68.0.0+incompatible // indirect
	github.com/Azure/go-autorest v14.2.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.30 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.24 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.13 // indirect
	github.com/Azure/go-autorest/autorest/azure/cli v0.4.7 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.1 // indirect
	github.com/Azure/go-autorest/autorest/to v0.4.1 // indirect
	github.com/Azure/go-autorest/autorest/validation v0.3.2 // indirect
	github.com/Azure/go-autorest/logger v0.2.2 // indirect
	github.com/Azure/go-autorest/tracing v0.6.1 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.5.0 // indirect
	github.com/Masterminds/sprig/v3 v3.3.0 // indirect
	github.com/TritonDataCenter/triton-go/v2 v2.0.0-pre4 // indirect
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/armon/go-radix v1.0.0 // indirect
	github.com/aws/aws-sdk-go-v2 v1.42.0 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.32.26 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.25 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.29 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.29 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.29 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.30 // indirect
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.310.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ecs v1.86.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.12 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.29 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.2.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.31.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.36.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.43.4 // indirect
	github.com/aws/smithy-go v1.27.3 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bgentry/speakeasy v0.2.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/denverdino/aliyungo v0.0.0-20230411124812-ab98a9173ace // indirect
	github.com/digitalocean/godo v1.197.0 // indirect
	github.com/dimchansky/utfbom v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.2 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.24.0 // indirect
	github.com/go-openapi/jsonreference v0.21.6 // indirect
	github.com/go-openapi/swag v0.27.0 // indirect
	github.com/go-openapi/swag/cmdutils v0.27.0 // indirect
	github.com/go-openapi/swag/conv v0.27.0 // indirect
	github.com/go-openapi/swag/fileutils v0.27.0 // indirect
	github.com/go-openapi/swag/jsonutils v0.27.0 // indirect
	github.com/go-openapi/swag/loading v0.27.0 // indirect
	github.com/go-openapi/swag/mangling v0.27.0 // indirect
	github.com/go-openapi/swag/netutils v0.27.0 // indirect
	github.com/go-openapi/swag/stringutils v0.27.0 // indirect
	github.com/go-openapi/swag/typeutils v0.27.0 // indirect
	github.com/go-openapi/swag/yamlutils v0.27.0 // indirect
	github.com/go-resty/resty/v2 v2.17.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/google/gnostic-models v0.7.1 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.17 // indirect
	github.com/googleapis/gax-go/v2 v2.22.0 // indirect
	github.com/gophercloud/gophercloud v1.14.1 // indirect
	github.com/hashicorp/consul/proto-public v0.8.1 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-discover/provider/gce v0.0.0-20260629062534-e8848307c996 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.2.0 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.7 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-7 // indirect
	github.com/hashicorp/mdns v1.0.7 // indirect
	github.com/hashicorp/vic v1.5.1-0.20260525083722-f33e5f4fdd75 // indirect
	github.com/huandu/xstrings v1.5.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/linode/linodego v1.69.1 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/pointerstructure v1.2.1 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nicolai86/scaleway-sdk v1.10.2-0.20180628010248-798f60e20bb2 // indirect
	github.com/opentracing/opentracing-go v1.2.1-0.20220228012449-10b1cf09e00b // indirect
	github.com/packethost/packngo v0.31.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/posener/complete v1.2.3 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.69.0 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/softlayer/softlayer-go v1.2.1 // indirect
	github.com/softlayer/xmlrpc v0.0.0-20200409220501-5f089df7cb7e // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common v1.3.126 // indirect
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm v1.3.121 // indirect
	github.com/vmware/govmomi v0.55.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.mongodb.org/mongo-driver v1.17.9 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/term v0.44.0 // indirect
	golang.org/x/tools v0.47.0 // indirect
	google.golang.org/api v0.287.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260630182238-925bb5da69e7 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.67.3 // indirect
	k8s.io/kube-openapi v0.0.0-20260624041617-8f3fa4921821 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.4.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
