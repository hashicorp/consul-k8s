package connectinject

import (
	ctx "context"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// In this test the Controller is started against a fake k8s clientset
// we create and update pods and validate the handlers are being reached
// from the controller's informer + queue processing algorithms
const (
	testPodName   = "test-pod"
	objectUpdated = "objectUpdated"
	objectCreated = "objectCreated"
)

type fakeHealthCheckHandler struct {
	ObjCreated bool
	ObjUpdated bool
	controller *HealthCheckController
}

// Init only calls the Reconciler and returns, this is tested in the handler test
func (f *fakeHealthCheckHandler) Init() error {
	// This triggers the reconciler in the handler
	return nil
}

func (f *fakeHealthCheckHandler) ObjectCreated(obj interface{}) error {
	f.ObjCreated = true
	return nil
}

func (f *fakeHealthCheckHandler) ObjectDeleted(obj interface{}) error {
	return nil
}

func (f *fakeHealthCheckHandler) ObjectUpdated(objNew interface{}) error {
	f.ObjUpdated = true
	return nil
}

func (f *fakeHealthCheckHandler) Reconcile() error {
	return nil
}

// This forces a Reconcile phase
func TestHealthCheckController_Init(t *testing.T) {
	// Controller.Init() just calls handler.Reconcile which is tested
	// individually inside the health_check_handler_test
	return
}

func testGetControllerAndStart(t *testing.T, start *corev1.Pod) (*HealthCheckController, chan struct{}) {
	stopCh := make(chan struct{})
	clientset := fake.NewSimpleClientset(start)
	fakeHandler := &fakeHealthCheckHandler{}
	controller := &HealthCheckController{
		Log:        hclog.Default(),
		Clientset:  clientset,
		Queue:      nil,
		Informer:   nil,
		SkipWait:   true,
		Handle:     fakeHandler,
		MaxRetries: 0,
	}
	go func() {
		controller.Run(stopCh)
	}()
	time.Sleep(time.Second * 1)
	return controller, stopCh
}

func TestHealthCheckController(t *testing.T) {
	cases := []struct {
		Name     string
		PodStart *corev1.Pod
		PodNext  *corev1.Pod
		Expected map[string]bool
		Err      string
	}{
		{
			"PodPending to PodRunning objectCreate",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testPodName,
					Namespace:   "default",
					Labels:      map[string]string{labelInject: "true"},
					Annotations: map[string]string{annotationInject: "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodPhase(corev1.PodPending),
					Conditions: nil,
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testPodName,
					Namespace:   "default",
					Labels:      map[string]string{labelInject: "true"},
					Annotations: map[string]string{annotationInject: "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					Phase:      corev1.PodPhase(corev1.PodRunning),
					Conditions: nil,
				},
			},
			map[string]bool{
				objectUpdated: true,
				objectCreated: true,
			},
			"",
		},
		{
			"PodUpdate from health check passing to fail",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testPodName,
					Namespace:   "default",
					Labels:      map[string]string{labelInject: "true"},
					Annotations: map[string]string{annotationInject: "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPhase(corev1.PodRunning),
					Conditions: []corev1.PodCondition{{
						Type:   "Ready",
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testPodName,
					Namespace:   "default",
					Labels:      map[string]string{labelInject: "true"},
					Annotations: map[string]string{annotationInject: "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPhase(corev1.PodRunning),
					Conditions: []corev1.PodCondition{{
						Type:   "Ready",
						Status: corev1.ConditionFalse,
					}},
				},
			},
			map[string]bool{
				objectUpdated: true,
				objectCreated: false,
			},
			"",
		},
		{
			"PodUpdate from health check fail to passing",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testPodName,
					Namespace:   "default",
					Labels:      map[string]string{labelInject: "true"},
					Annotations: map[string]string{annotationInject: "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPhase(corev1.PodRunning),
					Conditions: []corev1.PodCondition{{
						Type:   "Ready",
						Status: corev1.ConditionFalse,
					}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testPodName,
					Namespace:   "default",
					Labels:      map[string]string{labelInject: "true"},
					Annotations: map[string]string{annotationInject: "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPhase(corev1.PodRunning),
					Conditions: []corev1.PodCondition{{
						Type:   "Ready",
						Status: corev1.ConditionTrue,
					}},
				},
			},
			map[string]bool{
				objectUpdated: true,
				objectCreated: false,
			},
			"",
		},
		{
			"No PodUpdate with no change",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testPodName,
					Namespace:   "default",
					Labels:      map[string]string{labelInject: "true"},
					Annotations: map[string]string{annotationInject: "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPhase(corev1.PodRunning),
					Conditions: []corev1.PodCondition{{
						Type:   "Ready",
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        testPodName,
					Namespace:   "default",
					Labels:      map[string]string{labelInject: "true"},
					Annotations: map[string]string{annotationInject: "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPhase(corev1.PodRunning),
					Conditions: []corev1.PodCondition{{
						Type:   "Ready",
						Status: corev1.ConditionTrue,
					}},
				},
			},
			map[string]bool{
				objectUpdated: false,
				objectCreated: false,
			},
			"",
		},
		{
			"No Update without annotations or label",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPhase(corev1.PodRunning),
					Conditions: []corev1.PodCondition{{
						Type:   "Ready",
						Status: corev1.ConditionTrue,
					}},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: testPodName,
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPhase(corev1.PodRunning),
					Conditions: []corev1.PodCondition{{
						Type:   "Ready",
						Status: corev1.ConditionTrue,
					}},
				},
			},
			map[string]bool{
				objectUpdated: false,
				objectCreated: false,
			},
			"",
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			c, stopCh := testGetControllerAndStart(t, tt.PodStart)
			defer close(stopCh)
			hch := fakeHealthCheckHandler{}
			c.Handle = &hch
			testSetTransition(t, c, tt.PodNext)
			time.Sleep(time.Second * 3)
			actual := map[string]bool{
				objectCreated: hch.ObjCreated,
				objectUpdated: hch.ObjUpdated,
			}
			require.Equal(tt.Expected, actual)
		})
	}
}

func testSetTransition(t *testing.T, c *HealthCheckController, podNext *corev1.Pod) {
	require := require.New(t)
	_, err := c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Update(ctx.Background(), podNext, metav1.UpdateOptions{})
	require.NoError(err)
}
