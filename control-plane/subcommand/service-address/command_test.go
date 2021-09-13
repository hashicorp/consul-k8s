package serviceaddress

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

// Test that flags are validated.
func TestRun_FlagValidation(t *testing.T) {
	cases := []struct {
		Flags  []string
		ExpErr string
	}{
		{
			Flags:  []string{},
			ExpErr: "-k8s-namespace must be set",
		},
		{
			Flags:  []string{"-k8s-namespace=default"},
			ExpErr: "-name must be set",
		},
		{
			Flags:  []string{"-k8s-namespace=default", "-name=name"},
			ExpErr: "-output-file must be set",
		},
	}
	for _, c := range cases {
		t.Run(c.ExpErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			responseCode := cmd.Run(c.Flags)
			require.Equal(t, 1, responseCode, ui.ErrorWriter.String())
			require.Contains(t, ui.ErrorWriter.String(), c.ExpErr)
		})
	}
}

// Test that if the file can't be written to we return an error.
func TestRun_UnableToWriteToFile(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	k8sNS := "default"
	svcName := "service-name"
	expAddress := "1.2.3.4"

	// Create the service.
	k8s := fake.NewSimpleClientset()
	_, err := k8s.CoreV1().Services(k8sNS).Create(context.Background(), kubeLoadBalancerSvc(svcName, expAddress, ""), metav1.CreateOptions{})
	require.NoError(err)

	// Run command with an unwriteable file.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	responseCode := cmd.Run([]string{
		"-k8s-namespace", k8sNS,
		"-name", svcName,
		"-output-file", "/this/filepath/does/not/exist",
	})
	require.Equal(1, responseCode, ui.ErrorWriter.String())
	require.Contains(ui.ErrorWriter.String(),
		"Unable to write address to file: open /this/filepath/does/not/exist: no such file or directory")
}

func TestRun_UnresolvableHostname(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	k8sNS := "default"
	svcName := "service-name"

	// Create the service.
	k8s := fake.NewSimpleClientset()
	_, err := k8s.CoreV1().Services(k8sNS).Create(context.Background(), kubeLoadBalancerSvc(svcName, "", "unresolvable"), metav1.CreateOptions{})
	require.NoError(err)

	// Run command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)
	outputFile := filepath.Join(tmpDir, "address.txt")

	responseCode := cmd.Run([]string{
		"-k8s-namespace", k8sNS,
		"-name", svcName,
		"-output-file", outputFile,
		"-resolve-hostnames=true",
	})
	require.Equal(1, responseCode)
	require.Contains(ui.ErrorWriter.String(), "Unable to get service address: unable to resolve hostname:")
}

// Test running with different service types.
func TestRun_ServiceTypes(t *testing.T) {
	t.Parallel()

	// All services will have the name "service-name"
	cases := map[string]struct {
		Service              *v1.Service
		ServiceModificationF func(*v1.Service)
		ResolveHostnames     bool
		ExpErr               string
		ExpAddress           string
	}{
		"ClusterIP": {
			Service:    kubeClusterIPSvc("service-name"),
			ExpAddress: "5.6.7.8",
		},
		"NodePort": {
			Service: kubeNodePortSvc("service-name"),
			ExpErr:  "services of type NodePort are not supported",
		},
		"LoadBalancer IP": {
			Service:    kubeLoadBalancerSvc("service-name", "1.2.3.4", ""),
			ExpAddress: "1.2.3.4",
		},
		"LoadBalancer hostname": {
			Service:    kubeLoadBalancerSvc("service-name", "", "localhost"),
			ExpAddress: "localhost",
		},
		"LoadBalancer hostname with resolve-hostnames=true": {
			Service:          kubeLoadBalancerSvc("service-name", "", "localhost"),
			ResolveHostnames: true,
			ExpAddress:       "127.0.0.1",
		},
		"LoadBalancer IP and hostname": {
			Service:    kubeLoadBalancerSvc("service-name", "1.2.3.4", "example.com"),
			ExpAddress: "1.2.3.4",
		},
		"LoadBalancer first ingress empty": {
			Service: kubeLoadBalancerSvc("service-name", "1.2.3.4", "example.com"),
			ServiceModificationF: func(svc *v1.Service) {
				svc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{
					{},
					{
						IP: "5.6.7.8",
					},
				}
			},
			ExpAddress: "5.6.7.8",
		},
		"ExternalName": {
			Service: kubeExternalNameSvc("service-name"),
			ExpErr:  "services of type ExternalName are not supported",
		},
		"invalid name": {
			Service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service-name",
				},
				Spec: v1.ServiceSpec{
					Type: "invalid",
				},
			},
			ExpErr: "unknown service type \"invalid\"",
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			require := require.New(tt)
			k8sNS := "default"
			svcName := "service-name"

			// Create the service.
			k8s := fake.NewSimpleClientset()
			if c.ServiceModificationF != nil {
				c.ServiceModificationF(c.Service)
			}
			_, err := k8s.CoreV1().Services(k8sNS).Create(context.Background(), c.Service, metav1.CreateOptions{})
			require.NoError(err)

			// Run command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				k8sClient: k8s,
			}
			tmpDir, err := ioutil.TempDir("", "")
			require.NoError(err)
			defer os.RemoveAll(tmpDir)
			outputFile := filepath.Join(tmpDir, "address.txt")

			args := []string{
				"-k8s-namespace", k8sNS,
				"-name", svcName,
				"-output-file", outputFile,
			}
			if c.ResolveHostnames {
				args = append(args, "-resolve-hostnames=true")
			}
			responseCode := cmd.Run(args)
			if c.ExpErr != "" {
				require.Equal(1, responseCode)
				require.Contains(ui.ErrorWriter.String(), c.ExpErr)
			} else {
				require.Equal(0, responseCode, ui.ErrorWriter.String())
				actAddressBytes, err := ioutil.ReadFile(outputFile)
				require.NoError(err)
				require.Equal(c.ExpAddress, string(actAddressBytes))
			}
		})
	}
}

// Test that we write the address to file successfully, even when we have to retry
// looking up the service. This mimics what happens in Kubernetes when a
// service gets an ingress address after a cloud provider provisions a
// load balancer.
func TestRun_FileWrittenAfterRetry(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		// InitialService controls whether a service with that name will have
		// already been created. The service won't have an address yet.
		InitialService bool
		// UpdateDelay controls how long we wait before updating the service
		// with the UpdateIP address. NOTE: the retry duration for this
		// test is set to 10ms.
		UpdateDelay time.Duration
	}{
		"initial service exists": {
			InitialService: true,
			UpdateDelay:    50 * time.Millisecond,
		},
		"initial service does not exist, immediate update": {
			InitialService: false,
			UpdateDelay:    0,
		},
		"initial service does not exist, 50ms delay": {
			InitialService: false,
			UpdateDelay:    50 * time.Millisecond,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			k8sNS := "default"
			svcName := "service-name"
			ip := "1.2.3.4"
			k8s := fake.NewSimpleClientset()

			if c.InitialService {
				svc := kubeLoadBalancerSvc(svcName, "", "")
				// Reset the status to nothing.
				svc.Status = v1.ServiceStatus{}
				_, err := k8s.CoreV1().Services(k8sNS).Create(context.Background(), svc, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			// Create/update the service after delay.
			go func() {
				time.Sleep(c.UpdateDelay)
				svc := kubeLoadBalancerSvc(svcName, ip, "")
				var err error
				if c.InitialService {
					_, err = k8s.CoreV1().Services(k8sNS).Update(context.Background(), svc, metav1.UpdateOptions{})
				} else {
					_, err = k8s.CoreV1().Services(k8sNS).Create(context.Background(), svc, metav1.CreateOptions{})
				}
				require.NoError(t, err)
			}()

			// Run command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:            ui,
				k8sClient:     k8s,
				retryDuration: 10 * time.Millisecond,
			}
			tmpDir, err := ioutil.TempDir("", "")
			require.NoError(t, err)
			defer os.RemoveAll(tmpDir)
			outputFile := filepath.Join(tmpDir, "address.txt")

			responseCode := cmd.Run([]string{
				"-k8s-namespace", k8sNS,
				"-name", svcName,
				"-output-file", outputFile,
			})
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())
			actAddressBytes, err := ioutil.ReadFile(outputFile)
			require.NoError(t, err)
			require.Equal(t, ip, string(actAddressBytes))
		})
	}
}

func kubeLoadBalancerSvc(name string, ip string, hostname string) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ServiceSpec{
			Type:      "LoadBalancer",
			ClusterIP: "9.0.1.2",
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Protocol: "TCP",
					Port:     80,
					TargetPort: intstr.IntOrString{
						IntVal: 8080,
					},
					NodePort: 32001,
				},
			},
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{
						IP:       ip,
						Hostname: hostname,
					},
				},
			},
		},
	}
}

func kubeNodePortSvc(name string) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ServiceSpec{
			Type:      "NodePort",
			ClusterIP: "1.2.3.4",
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Protocol: "TCP",
					Port:     80,
					TargetPort: intstr.IntOrString{
						IntVal: 8080,
					},
					NodePort: 32000,
				},
			},
		},
	}
}

func kubeClusterIPSvc(name string) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ServiceSpec{
			Type:      "ClusterIP",
			ClusterIP: "5.6.7.8",
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Protocol: "TCP",
					Port:     80,
					TargetPort: intstr.IntOrString{
						IntVal: 8080,
					},
				},
			},
		},
	}
}

func kubeExternalNameSvc(name string) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ServiceSpec{
			Type:         "ExternalName",
			ExternalName: fmt.Sprintf("%s.example.com", name),
		},
	}
}
