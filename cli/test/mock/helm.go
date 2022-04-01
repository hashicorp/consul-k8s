package mock

import (
	"fmt"
	"io"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest/fake"
	"k8s.io/client-go/restmapper"
)

// FakeClient is a wrapper around k8s fake client to satisfy
// Helm's interface for the kubernetes client.
type FakeClient struct {
	K8sClient kubernetes.Interface
}

func (f FakeClient) Create(resources kube.ResourceList) (*kube.Result, error) {
	res := &kube.Result{}
	for _, r := range resources {
		_, err := resource.NewHelper(r.Client, r.Mapping).Create(r.Namespace, true, r.Object)
		if err != nil {
			return nil, err
		}
		res.Created = append(res.Created, r)
	}
	return res, nil
}

func (f FakeClient) Wait(resources kube.ResourceList, timeout time.Duration) error {
	return nil
}

func (f FakeClient) WaitWithJobs(resources kube.ResourceList, timeout time.Duration) error {
	return nil
}

func (f FakeClient) Delete(resources kube.ResourceList) (*kube.Result, []error) {
	res := &kube.Result{}
	var errs []error
	for _, r := range resources {
		_, err := resource.NewHelper(r.Client, r.Mapping).Delete(r.Namespace, r.Name)
		if err != nil {
			errs = append(errs, err)
		} else {
			res.Deleted = append(res.Deleted, r)
		}
	}
	return res, nil
}

func (f FakeClient) WatchUntilReady(resources kube.ResourceList, timeout time.Duration) error {
	return nil
}

func (f FakeClient) Update(original, target kube.ResourceList, force bool) (*kube.Result, error) {
	panic("implement me: update")
}

func (f FakeClient) Build(reader io.Reader, validate bool) (kube.ResourceList, error) {
	restMapper := meta.NewDefaultRESTMapper(nil)
	restMapper.Add(schema.GroupVersionKind{Group: "policy", Version: "v1beta1", Kind: "PodDisruptionBudget"}, TestScope{})
	restMapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"}, TestScope{})
	restMapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, TestScope{})
	restMapper.Add(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"}, TestScope{})
	restMapper.Add(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"}, TestScope{})
	restMapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"}, TestScope{})
	restMapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"}, TestScope{})
	restMapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}, TestScope{})

	builder := resource.NewFakeBuilder(
		func(version schema.GroupVersion) (resource.RESTClient, error) {
			return &fake.RESTClient{}, nil
		},
		func() (meta.RESTMapper, error) {
			return restMapper, nil
		},
		func() (restmapper.CategoryExpander, error) {
			return resource.FakeCategoryExpander, nil
		},
	)

	result, err := builder.
		Unstructured().
		Schema(AlwaysValidSchema{}).
		Stream(reader, "").
		ContinueOnError().
		Do().Infos()
	return result, err
}

func (f FakeClient) WaitAndGetCompletedPodPhase(name string, timeout time.Duration) (corev1.PodPhase, error) {
	panic("implement me: WaitAndGetCompletedPodPhase")
}

func (f FakeClient) IsReachable() error {
	return nil
}

// AlwaysValidSchema is always invalid.
type AlwaysValidSchema struct{}

// ValidateBytes always fails to validate.
func (AlwaysValidSchema) ValidateBytes([]byte) error {
	return nil
}

type TestScope struct{}

func (TestScope) Name() meta.RESTScopeName {
	return meta.RESTScopeNameNamespace
}

func CreateMockEnvSettings(t *testing.T, namespace string) *helmCLI.EnvSettings {
	return &helmCLI.EnvSettings{}
}

// CreateLogger creates a Helm logger from the testing struct.
func CreateLogger(t *testing.T) action.DebugLog {
	return func(s string, args ...interface{}) {
		msg := fmt.Sprintf(s, args...)

		t.Log(msg)
	}
}
