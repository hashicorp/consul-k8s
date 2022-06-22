module github.com/hashicorp/consul-k8s/control-plane/cni

require (
	github.com/containernetworking/cni v1.1.1
	github.com/containernetworking/plugins v1.1.1
	github.com/hashicorp/go-hclog v0.16.1
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/fatih/color v1.12.0 // indirect
	github.com/go-logr/logr v0.4.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-cmp v0.5.7 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/googleapis/gnostic v0.5.5 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/mattn/go-colorable v0.1.8 // indirect
	github.com/mattn/go-isatty v0.0.13 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/net v0.0.0-20211209124913-491a49abca63 // indirect
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/sys v0.0.0-20220412211240-33da011f77ad // indirect
	golang.org/x/term v0.0.0-20210220032956-6a3ed077a48d // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.26.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/klog/v2 v2.9.0 // indirect
	k8s.io/utils v0.0.0-20210819203725-bdf08cb9a70a // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.1.2 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)

replace github.com/hashicorp/consul/sdk v0.9.0 => github.com/hashicorp/consul/sdk v0.4.1-0.20220531155537-364758ef2f50

go 1.17
