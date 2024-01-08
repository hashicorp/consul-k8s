package webhookcertmanager

import (
	"context"
	"io/ioutil"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/hashicorp/consul-k8s/subcommand/webhook-cert-manager/mocks"
)

func TestRun_ExitsCleanlyOnSignals(t *testing.T) {
	t.Run("SIGINT", testSignalHandling(syscall.SIGINT))
	t.Run("SIGTERM", testSignalHandling(syscall.SIGTERM))
}

func testSignalHandling(sig os.Signal) func(*testing.T) {
	return func(t *testing.T) {
		webhookConfigOneName := "webhookOne"
		webhookConfigTwoName := "webhookTwo"

		caBundleOne := []byte("bootstrapped-CA-one")
		caBundleTwo := []byte("bootstrapped-CA-two")

		webhookOne := &admissionv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: webhookConfigOneName,
			},
			Webhooks: []admissionv1.MutatingWebhook{
				{
					Name: "webhook-under-test",
					ClientConfig: admissionv1.WebhookClientConfig{
						CABundle: caBundleOne,
					},
				},
			},
		}
		webhookTwo := &admissionv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: webhookConfigTwoName,
			},
			Webhooks: []admissionv1.MutatingWebhook{
				{
					Name: "webhookOne-under-test",
					ClientConfig: admissionv1.WebhookClientConfig{
						CABundle: caBundleTwo,
					},
				},
				{
					Name: "webhookTwo-under-test",
					ClientConfig: admissionv1.WebhookClientConfig{
						CABundle: caBundleTwo,
					},
				},
			},
		}

		k8s := fake.NewSimpleClientset(webhookOne, webhookTwo)
		ui := cli.NewMockUi()
		cmd := Command{
			UI:        ui,
			clientset: k8s,
		}
		cmd.init()

		file, err := ioutil.TempFile("", "config.json")
		require.NoError(t, err)
		defer os.Remove(file.Name())

		_, err = file.Write([]byte(configFile))
		require.NoError(t, err)

		exitCh := runCommandAsynchronously(&cmd, []string{
			"-config-file", file.Name(),
		})
		cmd.sendSignal(sig)

		// Assert that it exits cleanly or timeout.
		select {
		case exitCode := <-exitCh:
			require.Equal(t, 0, exitCode, ui.ErrorWriter.String())
		case <-time.After(time.Second * 1):
			// Fail if the signal was not caught.
			require.Fail(t, "timeout waiting for command to exit")
		}
	}
}

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  nil,
			expErr: "-config-file must be set",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(tt *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			exitCode := cmd.Run(c.flags)
			require.Equal(tt, 1, exitCode, ui.ErrorWriter.String())
			require.Contains(tt, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

func TestRun_SecretDoesNotExist(t *testing.T) {
	t.Parallel()
	secretOneName := "secret-deploy-1"
	secretTwoName := "secret-deploy-2"

	webhookConfigOneName := "webhookOne"
	webhookConfigTwoName := "webhookTwo"

	caBundleOne := []byte("bootstrapped-CA-one")
	caBundleTwo := []byte("bootstrapped-CA-two")

	webhookOne := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookConfigOneName,
		},
		Webhooks: []admissionv1.MutatingWebhook{
			{
				Name: "webhook-under-test",
				ClientConfig: admissionv1.WebhookClientConfig{
					CABundle: caBundleOne,
				},
			},
		},
	}
	webhookTwo := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookConfigTwoName,
		},
		Webhooks: []admissionv1.MutatingWebhook{
			{
				Name: "webhookOne-under-test",
				ClientConfig: admissionv1.WebhookClientConfig{
					CABundle: caBundleTwo,
				},
			},
			{
				Name: "webhookTwo-under-test",
				ClientConfig: admissionv1.WebhookClientConfig{
					CABundle: caBundleTwo,
				},
			},
		},
	}

	k8s := fake.NewSimpleClientset(webhookOne, webhookTwo)
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()

	file, err := ioutil.TempFile("", "config.json")
	require.NoError(t, err)
	defer os.Remove(file.Name())

	_, err = file.Write([]byte(configFile))
	require.NoError(t, err)

	exitCh := runCommandAsynchronously(&cmd, []string{
		"-config-file", file.Name(),
	})
	defer stopCommand(t, &cmd, exitCh)

	ctx := context.Background()
	timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		secretOne, err := k8s.CoreV1().Secrets("default").Get(ctx, secretOneName, metav1.GetOptions{})
		require.NoError(r, err)
		require.Equal(r, secretOne.Type, v1.SecretTypeTLS)

		secretTwo, err := k8s.CoreV1().Secrets("default").Get(ctx, secretTwoName, metav1.GetOptions{})
		require.NoError(r, err)
		require.Equal(r, secretTwo.Type, v1.SecretTypeTLS)

		webhookConfigOne, err := k8s.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, webhookConfigOneName, metav1.GetOptions{})
		require.NoError(r, err)
		require.NotEqual(r, webhookConfigOne.Webhooks[0].ClientConfig.CABundle, caBundleOne)

		webhookConfigTwo, err := k8s.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, webhookConfigTwoName, metav1.GetOptions{})
		require.NoError(r, err)
		require.NotEqual(r, webhookConfigTwo.Webhooks[0].ClientConfig.CABundle, caBundleTwo)
		require.NotEqual(r, webhookConfigTwo.Webhooks[1].ClientConfig.CABundle, caBundleTwo)
		require.Equal(r, webhookConfigTwo.Webhooks[0].ClientConfig.CABundle, webhookConfigTwo.Webhooks[1].ClientConfig.CABundle)
	})
}

func TestRun_SecretExists(t *testing.T) {
	t.Parallel()
	secretOneName := "secret-deploy-1"
	secretTwoName := "secret-deploy-2"

	webhookConfigOneName := "webhookOne"
	webhookConfigTwoName := "webhookTwo"

	caBundleOne := []byte("bootstrapped-CA-one")
	caBundleTwo := []byte("bootstrapped-CA-two")

	secretOne := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretOneName,
		},
		StringData: map[string]string{
			v1.TLSCertKey:       "cert-1",
			v1.TLSPrivateKeyKey: "private-key-1",
		},
		Type: v1.SecretTypeTLS,
	}
	secretTwo := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretTwoName,
		},
		StringData: map[string]string{
			v1.TLSCertKey:       "cert-2",
			v1.TLSPrivateKeyKey: "private-key-2",
		},
		Type: v1.SecretTypeTLS,
	}

	webhookOne := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookConfigOneName,
		},
		Webhooks: []admissionv1.MutatingWebhook{
			{
				Name: "webhook-under-test",
				ClientConfig: admissionv1.WebhookClientConfig{
					CABundle: caBundleOne,
				},
			},
		},
	}
	webhookTwo := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookConfigTwoName,
		},
		Webhooks: []admissionv1.MutatingWebhook{
			{
				Name: "webhookOne-under-test",
				ClientConfig: admissionv1.WebhookClientConfig{
					CABundle: caBundleTwo,
				},
			},
			{
				Name: "webhookTwo-under-test",
				ClientConfig: admissionv1.WebhookClientConfig{
					CABundle: caBundleTwo,
				},
			},
		},
	}

	k8s := fake.NewSimpleClientset(webhookOne, webhookTwo, secretOne, secretTwo)
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()

	file, err := ioutil.TempFile("", "config.json")
	require.NoError(t, err)
	defer os.Remove(file.Name())

	_, err = file.Write([]byte(configFile))
	require.NoError(t, err)

	exitCh := runCommandAsynchronously(&cmd, []string{
		"-config-file", file.Name(),
	})
	defer stopCommand(t, &cmd, exitCh)

	ctx := context.Background()
	timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		secretOne, err := k8s.CoreV1().Secrets("default").Get(ctx, secretOneName, metav1.GetOptions{})
		require.NoError(r, err)
		require.NotEqual(r, secretOne.Data[v1.TLSCertKey], []byte("cert-1"))
		require.NotEqual(r, secretOne.Data[v1.TLSPrivateKeyKey], []byte("private-key-1"))

		secretTwo, err := k8s.CoreV1().Secrets("default").Get(ctx, secretTwoName, metav1.GetOptions{})
		require.NoError(r, err)
		require.NotEqual(r, secretTwo.Data[v1.TLSCertKey], []byte("cert-2"))
		require.NotEqual(r, secretTwo.Data[v1.TLSPrivateKeyKey], []byte("private-key-2"))

		webhookConfigOne, err := k8s.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, webhookConfigOneName, metav1.GetOptions{})
		require.NoError(r, err)
		require.NotEqual(r, webhookConfigOne.Webhooks[0].ClientConfig.CABundle, caBundleOne)

		webhookConfigTwo, err := k8s.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, webhookConfigTwoName, metav1.GetOptions{})
		require.NoError(r, err)
		require.NotEqual(r, webhookConfigTwo.Webhooks[0].ClientConfig.CABundle, caBundleTwo)
		require.NotEqual(r, webhookConfigTwo.Webhooks[1].ClientConfig.CABundle, caBundleTwo)
		require.Equal(r, webhookConfigTwo.Webhooks[0].ClientConfig.CABundle, webhookConfigTwo.Webhooks[1].ClientConfig.CABundle)
	})
}

func TestRun_SecretUpdates(t *testing.T) {
	t.Parallel()
	secretOne := "secret-deploy-1"

	webhookConfigOne := "webhookOne"

	caBundleOne := []byte("bootstrapped-CA-one")

	secret1 := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretOne,
		},
		StringData: map[string]string{
			v1.TLSCertKey:       "cert-1",
			v1.TLSPrivateKeyKey: "private-key-1",
		},
		Type: v1.SecretTypeTLS,
	}

	webhookOne := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookConfigOne,
		},
		Webhooks: []admissionv1.MutatingWebhook{
			{
				Name: "webhook-under-test",
				ClientConfig: admissionv1.WebhookClientConfig{
					CABundle: caBundleOne,
				},
			},
		},
	}

	k8s := fake.NewSimpleClientset(webhookOne, secret1)
	ui := cli.NewMockUi()
	oneSec := 1 * time.Second

	cmd := Command{
		UI:         ui,
		clientset:  k8s,
		certExpiry: &oneSec,
	}
	cmd.init()

	file, err := ioutil.TempFile("", "config.json")
	require.NoError(t, err)
	defer os.Remove(file.Name())

	_, err = file.Write([]byte(configFileUpdates))
	require.NoError(t, err)

	exitCh := runCommandAsynchronously(&cmd, []string{
		"-config-file", file.Name(),
	})
	defer stopCommand(t, &cmd, exitCh)

	var certificate, key []byte

	ctx := context.Background()
	timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
	// First, check that the original secret contents are updated when the cert-manager starts.
	retry.RunWith(timer, t, func(r *retry.R) {
		secret1, err := k8s.CoreV1().Secrets("default").Get(ctx, secretOne, metav1.GetOptions{})
		require.NoError(r, err)
		require.NotEqual(r, secret1.Data[v1.TLSCertKey], []byte("cert-1"))
		require.NotEqual(r, secret1.Data[v1.TLSPrivateKeyKey], []byte("private-key-1"))

		certificate = secret1.Data[v1.TLSCertKey]
		key = secret1.Data[v1.TLSPrivateKeyKey]

		webhookConfig1, err := k8s.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, webhookConfigOne, metav1.GetOptions{})
		require.NoError(r, err)
		require.NotEqual(r, webhookConfig1.Webhooks[0].ClientConfig.CABundle, caBundleOne)
	})

	// Wait for certs to be rotated.
	time.Sleep(2 * time.Second)

	timer = &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
	// Check that the certificate is rotated and the secret is updated.
	retry.RunWith(timer, t, func(r *retry.R) {
		secret1, err := k8s.CoreV1().Secrets("default").Get(ctx, secretOne, metav1.GetOptions{})
		require.NoError(r, err)
		require.NotEqual(r, secret1.Data[v1.TLSCertKey], certificate)
		require.NotEqual(r, secret1.Data[v1.TLSPrivateKeyKey], key)
	})
}

// This test verifies that when there is an error while attempting to update
// the certs or the webhook config, it retries the update every second until
// it succeeds.
func TestCertWatcher(t *testing.T) {
	t.Parallel()

	webhookName := "webhookOne"
	webhook := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookName,
		},
		Webhooks: []admissionv1.MutatingWebhook{
			{
				Name:         "webhook-under-test",
				ClientConfig: admissionv1.WebhookClientConfig{},
			},
		},
	}
	certSource := &mocks.MockCertSource{}

	k8s := fake.NewSimpleClientset(webhook)
	ui := cli.NewMockUi()

	cmd := Command{
		UI:        ui,
		clientset: k8s,
		source:    certSource,
	}
	cmd.init()

	file, err := ioutil.TempFile("", "config.json")
	require.NoError(t, err)
	defer os.Remove(file.Name())

	_, err = file.Write([]byte(configFileUpdates))
	require.NoError(t, err)

	exitCh := runCommandAsynchronously(&cmd, []string{
		"-config-file", file.Name(),
	})
	defer stopCommand(t, &cmd, exitCh)

	ctx := context.Background()
	timer := &retry.Timer{Timeout: 5 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		webhookConfig, err := k8s.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, webhookName, metav1.GetOptions{})
		require.NoError(r, err)
		// Verify that the CA cert has been initally set on the MWC.
		require.Contains(r, string(webhookConfig.Webhooks[0].ClientConfig.CABundle), "ca-certificate-string")
	})
	// Update the CA bundle on the MWC to `""` to replicate a helm upgrade
	webhook.Webhooks[0].ClientConfig.CABundle = []byte("")
	_, err = k8s.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(ctx, webhook, metav1.UpdateOptions{})
	require.NoError(t, err)

	// If this test passes, it implies that the system has recovered from the MWC
	// getting updated to have the correct CA within a reasonable time window
	timer = &retry.Timer{Timeout: 5 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		webhookConfig, err := k8s.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, webhookName, metav1.GetOptions{})
		require.NoError(r, err)
		// Verify that the CA cert has been updated with the correct CA.
		require.Contains(r, string(webhookConfig.Webhooks[0].ClientConfig.CABundle), "ca-certificate-string")
	})
}

func TestValidate(t *testing.T) {
	t.Parallel()
	webhook := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "webhook-config-name",
		},
	}
	client := fake.NewSimpleClientset(webhook)

	cases := map[string]struct {
		config    webhookConfig
		clientset kubernetes.Interface
		expErr    string
	}{
		"name": {
			config: webhookConfig{
				Name:            "",
				TLSAutoHosts:    []string{"host-1", "host-2"},
				SecretName:      "secret-name",
				SecretNamespace: "default",
			},
			clientset: client,
			expErr:    `config.Name cannot be ""`,
		},
		"nonExistantMWC": {
			config: webhookConfig{
				Name:            "webhook-config-name",
				TLSAutoHosts:    []string{"host-1", "host-2"},
				SecretName:      "secret-name",
				SecretNamespace: "default",
			},
			clientset: fake.NewSimpleClientset(),
			expErr:    `MutatingWebhookConfiguration with name "webhook-config-name" must exist in cluster`,
		},
		"secretName": {
			config: webhookConfig{
				Name:            "webhook-config-name",
				TLSAutoHosts:    []string{"host-1", "host-2"},
				SecretName:      "",
				SecretNamespace: "default",
			},
			clientset: client,
			expErr:    `config.SecretName cannot be ""`,
		},
		"secretNameSpace": {
			config: webhookConfig{
				Name:            "webhook-config-name",
				TLSAutoHosts:    []string{"host-1", "host-2"},
				SecretName:      "secret-name",
				SecretNamespace: "",
			},
			clientset: client,
			expErr:    `config.SecretNameSpace cannot be ""`,
		},
		"multi-error": {
			config: webhookConfig{
				Name:            "",
				TLSAutoHosts:    []string{},
				SecretName:      "",
				SecretNamespace: "",
			},
			expErr: `config.Name cannot be "", config.SecretName cannot be "", config.SecretNameSpace cannot be ""`,
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			err := c.config.validate(context.Background(), c.clientset)
			require.EqualError(tt, err, c.expErr)
		})
	}
}

// This function starts the command asynchronously and returns a non-blocking chan.
// When finished, the command will send its exit code to the channel.
// Note that it's the responsibility of the caller to terminate the command by calling stopCommand,
// otherwise it can run forever.
func runCommandAsynchronously(cmd *Command, args []string) chan int {
	// We have to run cmd.init() to ensure that the channel the command is
	// using to watch for os interrupts is initialized. If we don't do this,
	// then if stopCommand is called immediately, it will block forever
	// because it calls interrupt() which will attempt to send on a nil channel.
	cmd.init()
	exitChan := make(chan int, 1)

	go func() {
		exitChan <- cmd.Run(args)
	}()

	return exitChan
}

func stopCommand(t *testing.T, cmd *Command, exitChan chan int) {
	if len(exitChan) == 0 {
		cmd.interrupt()
	}
	select {
	case c := <-exitChan:
		require.Equal(t, 0, c, string(cmd.UI.(*cli.MockUi).ErrorWriter.Bytes()))
	}
}

const configFile = `[
  {
    "name": "webhookOne",
    "tlsAutoHosts": [
      "foo",
      "bar",
      "baz"
    ],
    "secretName": "secret-deploy-1",
    "secretNamespace": "default"
  },
  {
    "name": "webhookTwo",
    "tlsAutoHosts": [
      "foo-2",
      "bar-3",
      "baz-4"
    ],
    "secretName": "secret-deploy-2",
    "secretNamespace": "default"
  }
]`

const configFileUpdates = `[
  {
    "name": "webhookOne",
    "tlsAutoHosts": [
      "foo",
      "bar",
      "baz"
    ],
    "secretName": "secret-deploy-1",
    "secretNamespace": "default"
  }
]`
