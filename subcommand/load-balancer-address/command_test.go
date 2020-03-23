package loadbalanceraddress

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	_, err := k8s.CoreV1().Services(k8sNS).Create(kubeSvc(svcName, expAddress, ""))
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

// Test running with different permutations of ingress.
func TestRun_LoadBalancerIngressPermutations(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	cases := map[string]struct {
		Ingress    []v1.LoadBalancerIngress
		ExpAddress string
	}{
		"ip": {
			Ingress: []v1.LoadBalancerIngress{
				{
					IP: "1.2.3.4",
				},
			},
			ExpAddress: "1.2.3.4",
		},
		"hostname": {
			Ingress: []v1.LoadBalancerIngress{
				{
					Hostname: "example.com",
				},
			},
			ExpAddress: "example.com",
		},
		"empty first ingress": {
			Ingress: []v1.LoadBalancerIngress{
				{},
				{
					IP: "1.2.3.4",
				},
			},
			ExpAddress: "1.2.3.4",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			k8sNS := "default"
			svcName := "service-name"

			// Create the service.
			k8s := fake.NewSimpleClientset()
			svc := &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: svcName,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: c.Ingress,
					},
				},
			}
			_, err := k8s.CoreV1().Services(k8sNS).Create(svc)
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
			})
			require.Equal(0, responseCode, ui.ErrorWriter.String())
			actAddressBytes, err := ioutil.ReadFile(outputFile)
			require.NoError(err)
			require.Equal(c.ExpAddress, string(actAddressBytes))

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
				_, err := k8s.CoreV1().Services(k8sNS).Create(&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: svcName,
					},
				})
				require.NoError(t, err)
			}

			// Create/update the service after delay.
			go func() {
				time.Sleep(c.UpdateDelay)
				svc := kubeSvc(svcName, ip, "")
				var err error
				if c.InitialService {
					_, err = k8s.CoreV1().Services(k8sNS).Update(svc)
				} else {
					_, err = k8s.CoreV1().Services(k8sNS).Create(svc)
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

func kubeSvc(name string, ip string, hostname string) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
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
