package connectinject

import (
	ctx "context"
	"fmt"
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

var ObjCreated bool
var ObjUpdated_passing bool
var ObjUpdated_failing bool
var ObjUpdated_unknown bool

type fakeHealthCheckHandler struct {
	ObjCreated         bool
	ObjReconciled      bool
	ObjUpdated_passing bool
	ObjUpdated_failing bool
	ObjUpdated_unknown bool
	controller         *HealthCheckController
}

// TODO: figure out how to test reconciler from here
func (f fakeHealthCheckHandler) Init() error {
	// This triggers the reconciler
	return nil
}

func (f fakeHealthCheckHandler) ObjectCreated(obj interface{}) error {
	ObjCreated = true
	return nil
}

func (f fakeHealthCheckHandler) ObjectDeleted(obj interface{}) error {
	return nil
}

func (f fakeHealthCheckHandler) ObjectUpdated(objNew interface{}) error {
	pod := objNew.(*corev1.Pod)
	status := f.controller.getReadyStatus(pod)
	if status == corev1.ConditionTrue {
		ObjUpdated_passing = true
	} else if status == corev1.ConditionFalse {
		ObjUpdated_failing = true
	} else {
		ObjUpdated_unknown = true
		return fmt.Errorf("unknown status! %v", status)
	}
	return nil
}

func (f fakeHealthCheckHandler) Reconcile() error {
	return nil
}

func testPod(name string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Labels:      map[string]string{labelInject: "true"},
			Annotations: map[string]string{annotationInject: "true"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				corev1.Container{
					Name: name,
				},
			},
		},
	}
}

// This forces a Reconcile phase
func TestHealthCheckController_Init(t *testing.T) {
	// Controller.Init() just calls handler.Reconcile which is tested
	// individually inside the health_check_handler_test
	return
}

// Run tests a suite that validates Create/Update paths are caught
// by the informer, filtered, and passed to the handler
func TestHealthCheckController_Run(t *testing.T) {

	clientset := fake.NewSimpleClientset()
	fakeHandler := &fakeHealthCheckHandler{}
	// setup the controller
	controller := &HealthCheckController{
		Log:        hclog.Default(),
		Clientset:  clientset,
		Queue:      nil,
		Informer:   nil,
		Handle:     fakeHandler,
		MaxRetries: 0,
	}
	fakeHandler.controller = controller

	context, cancelFunc := ctx.WithCancel(ctx.Background())
	defer cancelFunc()
	healthCh := make(chan struct{})
	go func() {
		defer close(healthCh)
		controller.Run(context.Done())
	}()
	time.Sleep(time.Second * 3)
	testRunNewPod(t, controller)
	testRunUpdatePodFailing(t, controller)
	testRunUpdatePodPassing(t, controller)
}
func reset() {
	ObjCreated = false
	ObjUpdated_unknown = false
	ObjUpdated_passing = false
	ObjUpdated_failing = false
}

// testRunNewPod creates a new Pod, then simulates scheduling and running
// by updating it to phase PodScheduling then PodRunning
func testRunNewPod(t *testing.T, c *HealthCheckController) {
	require := require.New(t)
	reset()

	podName := "test-pod-create"
	pod := testPod(podName)
	_, err := c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Create(ctx.Background(), &pod, metav1.CreateOptions{})
	require.NoError(err)
	podget, err := c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Get(ctx.Background(), podName, metav1.GetOptions{})
	require.NoError(err)
	podget.Status.Phase = corev1.PodPending
	_, err = c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Update(ctx.Background(), podget, metav1.UpdateOptions{})
	require.NoError(err)
	podget, err = c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Get(ctx.Background(), podName, metav1.GetOptions{})
	require.NoError(err)
	podget.Status.Phase = corev1.PodRunning
	_, err = c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Update(ctx.Background(), podget, metav1.UpdateOptions{})
	require.NoError(err)
	time.Sleep(time.Second * 1)
	require.Equal(true, ObjCreated)
	require.Equal(true, ObjUpdated_passing)

}

// testRunUpdatePodFailing creates a new Pod and then marks it failed
func testRunUpdatePodFailing(t *testing.T, c *HealthCheckController) {
	require := require.New(t)
	reset()

	podName := "test-pod-failing"
	pod := testPod(podName)
	_, err := c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Create(ctx.Background(), &pod, metav1.CreateOptions{})
	require.NoError(err)
	podget, err := c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Get(ctx.Background(), podName, metav1.GetOptions{})
	require.NoError(err)
	podget.Status.Phase = corev1.PodPending
	_, err = c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Update(ctx.Background(), podget, metav1.UpdateOptions{})
	require.NoError(err)
	podget, err = c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Get(ctx.Background(), podName, metav1.GetOptions{})
	require.NoError(err)
	podget.Status.Phase = corev1.PodRunning
	podget.Status.Conditions = findAndReplaceConditionStatus(podget.Status.Conditions, corev1.ConditionFalse)
	_, err = c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Update(ctx.Background(), podget, metav1.UpdateOptions{})
	require.NoError(err)
	time.Sleep(time.Second * 1)
	require.Equal(true, ObjCreated)
	require.Equal(true, ObjUpdated_failing)
	require.Equal(false, ObjUpdated_passing)
	require.Equal(false, ObjUpdated_unknown)
}

// testRunUpdatePodPassing uses the failed pod from testRunUpdatePodFailing
// and marks it passing
func testRunUpdatePodPassing(t *testing.T, c *HealthCheckController) {
	require := require.New(t)
	reset()

	podName := "test-pod-failing"
	podget, err := c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Get(ctx.Background(), podName, metav1.GetOptions{})
	require.NoError(err)
	podget.Status.Conditions = findAndReplaceConditionStatus(podget.Status.Conditions, corev1.ConditionTrue)
	_, err = c.Clientset.CoreV1().Pods(metav1.NamespaceDefault).Update(ctx.Background(), podget, metav1.UpdateOptions{})
	require.NoError(err)
	time.Sleep(time.Second * 1)
	require.Equal(true, ObjUpdated_passing)
	require.Equal(false, ObjCreated)
	require.Equal(false, ObjUpdated_failing)
	require.Equal(false, ObjUpdated_unknown)
}

func findAndReplaceConditionStatus(cs []corev1.PodCondition, status corev1.ConditionStatus) []corev1.PodCondition {
	var ret []corev1.PodCondition
	found := false
	for _, cond := range cs {
		if cond.Type == "Ready" {
			found = true
			cond.Status = status
		}
		ret = append(ret, cond)
	}
	if !found {
		ret = append(ret, corev1.PodCondition{
			Type:    "Ready",
			Status:  status,
			Reason:  "test",
			Message: "test",
		})
	}
	return ret
}
