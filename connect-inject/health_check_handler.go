package connectinject

// handler skeleton inspired by https://github.com/trstringer/k8s-controller-core-resource

import (
	ctx "context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-k8s/subcommand/flags"
	"github.com/hashicorp/consul/api"
	log "github.com/hashicorp/go-hclog"
	corev1 "k8s.io/api/core/v1"
)

// HCHandler interface contains the methods that are required
type HCHandler interface {
	Init() error
	ObjectCreated(obj interface{}) error
	ObjectDeleted(obj interface{}) error
	ObjectUpdated(objNew interface{}) error
	Reconcile() error
}

const (
	healthCheckPassing  = "passing"
	healthCheckCritical = "critical"
)

// HealthCheckHandler is a sample implementation of HCHandler
type HealthCheckHandler struct {
	Log                log.Logger
	AclConfig          api.NamespaceACLConfig
	Client             *api.Client
	HFlags             *flags.HTTPFlags
	Clientset          kubernetes.Interface
	ConsulClientScheme string
	ConsulPort         string
}

// getConsulHealthCheckID deterministically generates a health check ID that will be unique to the Agent
// where the health check is registered and deregistered.
func (t *HealthCheckHandler) getConsulHealthCheckID(pod *corev1.Pod) string {
	return fmt.Sprintf("%s_%s_kubernetes-health-check-ttl", pod.Namespace, t.getConsulServiceID(pod))
}

// return the serviceID of the connect service
func (t *HealthCheckHandler) getConsulServiceID(pod *corev1.Pod) string {
	return pod.Name + "-" + pod.Annotations[annotationService]
	// TODO: I'd like to have this put into an annotation by this point, unfortunately it cannot
	// be injected by the webhook because pod.Name isnt determined by then
	//return pod.Annotations[annotationServiceID]
}

// updateConsulClient updates the Consul Client metadata to point to a new hostIP:port
// which is the IP of the host that the Pod runs on, in order to make Agent calls locally
// for health check registration/deregistration.
func (t *HealthCheckHandler) updateConsulClient(pod *corev1.Pod) error {
	var err error
	// We use the whole schema including http/https here because that is normally set in the
	// CONSUL_HTTP_ADDR environment variable, without https the client will default to http even if the port
	// is 8501
	httpFmt := "http"
	if t.ConsulPort == "8501" {
		httpFmt = "https"
	}
	newAddr := fmt.Sprintf("%s://%s:%s", httpFmt, pod.Status.HostIP, t.ConsulPort)
	t.HFlags.SetAddress(newAddr)

	// Set client api to point to the new host IP
	// TODO: APIClient() reads the ENV, see if we can avoid this for perf
	t.Client, err = t.HFlags.APIClient()
	if err != nil {
		t.Log.Error("unable to get Consul API Client for address %s: %s", newAddr, err)
		t.Client = nil
	}
	t.Log.Info("setting consul client to the following agent: %v", newAddr)
	return err
}

// registerConsulHealthCheck registers a Failing TTL health check for the service on this Agent.
// The Agent is local to the Pod which has failed a health check.
// This has the effect of marking the endpoint unhealthy for Consul service mesh traffic.
func (t *HealthCheckHandler) registerConsulHealthCheck(pod *corev1.Pod, consulHealthCheckID, serviceID, initialStatus, reason string) error {
	t.Log.Debug("registerConsulHealthCheck, %v %v", consulHealthCheckID, serviceID)
	// There is a chance of a race between when the Pod is transitioned to healthy by k8s and when we've initially
	// completed the registration of the service with the Consul Agent on this node. Retry a few times to be sure
	// that the service does in fact exist, otherwise it will return 500 from Consul API.
	retries := 0
	var err error
	err = backoff.Retry(func() error {
		if retries > 10 {
			err = fmt.Errorf("did not find serviceID: %v", serviceID)
			return nil
		}
		retries++
		services, err := t.Client.Agent().Services()
		if err == nil {
			for _, svc := range services {
				if svc.Service == serviceID {
					return nil
				}
			}
			return fmt.Errorf("did not find serviceID: %v", serviceID)
		}
		return err
	}, backoff.NewConstantBackOff(1*time.Second))
	if err != nil {
		// We were unable to find the service on this host, this is due to :
		// 1. the pod is no longer on this host, has moved or was deregistered from the Agent by Consul
		// 2. Consul isn't working properly
		return err
	}

	// Now create a TTL health check in Consul associated with this service.
	err = t.Client.Agent().CheckRegister(&api.AgentCheckRegistration{
		Name:      consulHealthCheckID,
		Notes:     "Kubernetes Health Check " + reason,
		ServiceID: serviceID,
		AgentServiceCheck: api.AgentServiceCheck{
			TTL:                            "100000h",
			Status:                         initialStatus,
			Notes:                          reason,
			TLSSkipVerify:                  true,
			SuccessBeforePassing:           1,
			FailuresBeforeCritical:         1,
			DeregisterCriticalServiceAfter: "",
		},
		// For Consul-ENT only, this will be omitted for OSS, aka ""
		Namespace: pod.Annotations[annotationConsulDestinationNamespace],
	})
	if err != nil {
		t.Log.Error("unable to register health check with Consul from k8s: %v", err)
		return err
	}
	return nil
}

// setConsulHealthCheckStatus will update the TTL status of the check
func (t *HealthCheckHandler) setConsulHealthCheckStatus(healthCheckID, reason string, fail bool) error {
	if fail {
		// TODO: Use UpdateTTL() instead for both of these
		return t.Client.Agent().FailTTL(healthCheckID, reason)
	} else {
		return t.Client.Agent().PassTTL(healthCheckID, reason)
	}
}

// Init handles any handler initialization and is a no-op
func (t *HealthCheckHandler) Init() error {
	t.Log.Debug("handlerInit starting")
	return t.Reconcile()
}

// Reconcile iterates through all Pods with the appropriate label and compares the
// current health check status against that which is stored in Consul and updates
// the consul health check accordingly. if the health check doesnt yet exist it will
func (t *HealthCheckHandler) Reconcile() error {
	podList, err := t.Clientset.CoreV1().Pods(corev1.NamespaceAll).List(ctx.Background(),
		metav1.ListOptions{LabelSelector: labelInject})
	if err != nil {
		t.Log.Error("unable to get pods failing handler, err:%v", err)
		return err
	}
	for _, pod := range podList.Items {
		t.Log.Debug("processing Pod %v", pod.Name)
		if pod.Status.Phase != corev1.PodRunning {
			t.Log.Info("pod %v is not running", pod.Name, pod.Status.Phase)
			continue
		}
		// 1. Fetch the health checks for this agent
		err = t.updateConsulClient(&pod)
		if err != nil {
			t.Log.Error("unable to set client connection for %v", pod.Name)
			continue
		}
		serviceID := t.getConsulServiceID(&pod)
		healthCheckID := t.getConsulHealthCheckID(&pod)

		// 2. Fetch the health checks on this agent related to this healthCheckID
		filter := "Name == `" + healthCheckID + "`"
		checks, err := t.Client.Agent().ChecksWithFilter(filter)
		if err != nil {
			t.Log.Error("unable to get agent health checks for %v", healthCheckID, filter)
			continue
		}
		if checks == nil || len(checks) == 0 {
			// TODO we need to handle this
			t.Log.Error("======== we really shouldn't be here!, %v", checks)
			continue
		}
		status, reason, err := t.getReadyStatusAndReason(&pod)
		if err != nil {
			t.Log.Error("unable to get pod status: %v", err)
			continue
		}
		initialStatus := healthCheckPassing
		if _, ok := checks[healthCheckID]; !ok {
			// 3. If the check doesnt exist yet, create it!
			if status == corev1.ConditionFalse {
				initialStatus = healthCheckCritical
			} else {
				// no need to store a reason if passing
				reason = ""
			}
			t.Log.Info("registering new health check for %v", pod.Name)
			err = t.registerConsulHealthCheck(&pod, healthCheckID, serviceID, initialStatus, reason)
			if err != nil {
				t.Log.Error("unable to register health check %v", err)
			}
			continue
		} else {
			// 3. check does exist, update it if necessary
			checkStatus := checks[healthCheckID].Status
			fail := true
			if checkStatus == healthCheckCritical && status == corev1.ConditionTrue {
				// set to passing
				fail = false
				reason = ""
			}
			if (checkStatus == healthCheckPassing && status == corev1.ConditionFalse) || fail == false {
				t.Log.Info("updating pod check for %v", pod.Name)
				// Update the health check!
				err = t.setConsulHealthCheckStatus(healthCheckID, reason, fail)
				if err != nil {
					t.Log.Error("unable to update health check status for %v", pod.Name)
					continue
				}
			}
		}
		t.Log.Debug("no update required for %V", pod.Name)
	}
	return nil
}

func (t *HealthCheckHandler) getReadyStatusAndReason(pod *corev1.Pod) (status corev1.ConditionStatus, reason string, err error) {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == "Ready" {
			return cond.Status, cond.Reason, nil
		}
	}
	return corev1.ConditionTrue, "", fmt.Errorf("unable to get pod ready status and reason for %v", pod.Name)
}

// ObjectCreated is called when a Pod transitions from Pending to Running
func (t *HealthCheckHandler) ObjectCreated(obj interface{}) error {
	var err error
	pod := obj.(*corev1.Pod)

	consulHealthCheckID := t.getConsulHealthCheckID(pod)
	err = t.updateConsulClient(pod)
	if err != nil {
		t.Log.Error("unable to update Consul client: %v", err)
		return err
	}

	err = t.registerConsulHealthCheck(pod, consulHealthCheckID, t.getConsulServiceID(pod), healthCheckPassing, "")
	if err != nil {
		t.Log.Error("unable to register health check: %v", err)
		return err
	}
	t.Log.Info("ObjectCreated %v", pod.Name, consulHealthCheckID)
	return err
}

// ObjectDeleted is a no-op because this is handled by the preStop check
func (t *HealthCheckHandler) ObjectDeleted(obj interface{}) error {
	return nil
}

// ObjectUpdated will determine if this is an update to healthy or unhealthy, we have already
// filtered out if this is a non health related transitional update in the controller so all Updates
// represent the desired final health check state (objNew)
// In the case of a transition TO healthy we mark the TTL as passing
// In the case of transition FROM healthy we mark the TTL as failing
func (t *HealthCheckHandler) ObjectUpdated(objNew interface{}) error {
	pod := objNew.(*corev1.Pod)
	t.Log.Debug("ObjectUpdated, %v %v", pod.Status.HostIP, pod.Status.Conditions)

	err := t.updateConsulClient(pod)
	if err != nil {
		t.Log.Error("unable to update Consul client: %v", err)
		return err
	}
	// This is the health check ID that we will update
	consulHealthCheckID := t.getConsulHealthCheckID(pod)

	for _, y := range pod.Status.Conditions {
		if y.Type == "Ready" && y.Status != corev1.ConditionTrue {
			// Set the status of the TTL health check to failed!
			err = t.setConsulHealthCheckStatus(consulHealthCheckID, y.Message, true)
			if err != nil {
				t.Log.Error("unable to update health check to fail: %v", err)
			}
			break
		} else {
			if y.Type == "Ready" && y.Status == corev1.ConditionTrue {
				// Set the Consul TTL to passing for this Pod
				err = t.setConsulHealthCheckStatus(consulHealthCheckID, y.Message, false)
				if err != nil {
					t.Log.Error("unable to update health check to pass: %v", err)
				}
				break
			}
		}
	}
	t.Client = nil
	// TODO: how to drop the client connection cleanly?
	if err == nil {
		t.Log.Debug("ObjectUpdated, %v %v", pod.Status.HostIP, pod.Status.Conditions)
	}
	return err
}
