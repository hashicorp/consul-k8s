package install

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	cmnFlag "github.com/hashicorp/consul-k8s/cli/common/flag"
	"github.com/hashicorp/consul-k8s/cli/helm"
	"github.com/hashicorp/consul-k8s/cli/release"
	"github.com/hashicorp/go-hclog"
	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckForPreviousPVCs(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-server-test1",
		},
	}
	pvc2 := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-server-test2",
		},
	}
	c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.Background(), pvc, metav1.CreateOptions{})
	c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.Background(), pvc2, metav1.CreateOptions{})
	err := c.checkForPreviousPVCs()
	require.Error(t, err)
	require.Equal(t, err.Error(), "found persistent volume claims from previous installations, delete before reinstalling: default/consul-server-test1,default/consul-server-test2")

	// Clear out the client and make sure the check now passes.
	c.kubernetes = fake.NewSimpleClientset()
	err = c.checkForPreviousPVCs()
	require.NoError(t, err)

	// Add a new irrelevant PVC and make sure the check continues to pass.
	pvc = &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "irrelevant-pvc",
		},
	}
	c.kubernetes.CoreV1().PersistentVolumeClaims("default").Create(context.Background(), pvc, metav1.CreateOptions{})
	err = c.checkForPreviousPVCs()
	require.NoError(t, err)
}

func TestCheckForPreviousSecrets(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		releaseName string
		helmValues  helm.Values
		secret      *v1.Secret
		expectMsg   bool
		expectErr   bool
	}{
		"No secrets, none expected": {
			releaseName: "consul",
			helmValues:  helm.Values{},
			secret:      nil,
			expectMsg:   true,
			expectErr:   false,
		},
		"Non-Consul secrets, none expected": {
			releaseName: "consul",
			helmValues:  helm.Values{},
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "non-consul-secret",
				},
			},
			expectMsg: true,
			expectErr: false,
		},
		"Consul secrets, none expected": {
			releaseName: "consul",
			helmValues:  helm.Values{},
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "consul-secret",
					Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
				},
			},
			expectMsg: false,
			expectErr: true,
		},
		"Federation secret, expected": {
			releaseName: "consul",
			helmValues: helm.Values{
				Global: helm.Global{
					Datacenter: "dc2",
					Federation: helm.Federation{
						Enabled:                true,
						PrimaryDatacenter:      "dc1",
						CreateFederationSecret: false,
					},
					Acls: helm.Acls{
						ReplicationToken: helm.ReplicationToken{
							SecretName: "consul-federation",
						},
					},
				},
			},
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "consul-federation",
					Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
				},
			},
			expectMsg: true,
			expectErr: false,
		},
		"No federation secret, but expected": {
			releaseName: "consul",
			helmValues: helm.Values{
				Global: helm.Global{
					Datacenter: "dc2",
					Federation: helm.Federation{
						Enabled:                true,
						PrimaryDatacenter:      "dc1",
						CreateFederationSecret: false,
					},
					Acls: helm.Acls{
						ReplicationToken: helm.ReplicationToken{
							SecretName: "consul-federation",
						},
					},
				},
			},
			secret:    nil,
			expectMsg: false,
			expectErr: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := getInitializedCommand(t)
			c.kubernetes = fake.NewSimpleClientset()

			c.kubernetes.CoreV1().Secrets("consul").Create(context.Background(), tc.secret, metav1.CreateOptions{})

			release := release.Release{Name: tc.releaseName, Configuration: tc.helmValues}
			msg, err := c.checkForPreviousSecrets(release)

			require.Equal(t, tc.expectMsg, msg != "")
			require.Equal(t, tc.expectErr, err != nil)
		})
	}
}

// TestValidateFlags tests the validate flags function.
func TestValidateFlags(t *testing.T) {
	// The following cases should all error, if they fail to this test fails.
	testCases := []struct {
		description string
		input       []string
	}{
		{
			"Should disallow non-flag arguments.",
			[]string{"foo", "-auto-approve"},
		},
		{
			"Should disallow specifying both values file AND presets.",
			[]string{"-f='f.txt'", "-preset=demo"},
		},
		{
			"Should error on invalid presets.",
			[]string{"-preset=foo"},
		},
		{
			"Should error on invalid timeout.",
			[]string{"-timeout=invalid-timeout"},
		},
		{
			"Should error on an invalid namespace. If this failed, TestValidLabel() probably did too.",
			[]string{"-namespace=\" nsWithSpace\""},
		},
		{
			"Should have errored on a non-existant file.",
			[]string{"-f=\"does_not_exist.txt\""},
		},
	}

	for _, testCase := range testCases {
		c := getInitializedCommand(t)
		t.Run(testCase.description, func(t *testing.T) {
			if err := c.validateFlags(testCase.input); err == nil {
				t.Errorf("Test case should have failed.")
			}
		})
	}
}

// getInitializedCommand sets up a command struct for tests.
func getInitializedCommand(t *testing.T) *Command {
	t.Helper()
	log := hclog.New(&hclog.LoggerOptions{
		Name:   "cli",
		Level:  hclog.Info,
		Output: os.Stdout,
	})

	baseCommand := &common.BaseCommand{
		Log: log,
	}

	c := &Command{
		BaseCommand: baseCommand,
	}
	c.init()
	return c
}

func TestCheckValidEnterprise(t *testing.T) {
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-secret",
		},
	}
	secret2 := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consul-secret2",
		},
	}

	// Enterprise secret is valid.
	c.kubernetes.CoreV1().Secrets("consul").Create(context.Background(), secret, metav1.CreateOptions{})
	err := c.checkValidEnterprise(secret.Name)
	require.NoError(t, err)

	// Enterprise secret does not exist.
	err = c.checkValidEnterprise("consul-unrelated-secret")
	require.Error(t, err)
	require.Contains(t, err.Error(), "please make sure that the secret exists")

	// Enterprise secret exists in a different namespace.
	c.kubernetes.CoreV1().Secrets("unrelated").Create(context.Background(), secret2, metav1.CreateOptions{})
	err = c.checkValidEnterprise(secret2.Name)
	require.Error(t, err)
	require.Contains(t, err.Error(), "please make sure that the secret exists")
}

func TestTaskCreateCommand_AutocompleteFlags(t *testing.T) {
	t.Parallel()
	cmd := getInitializedCommand(t)

	predictor := cmd.AutocompleteFlags()

	// Test that we get the expected number of predictions
	args := complete.Args{Last: "-"}
	res := predictor.Predict(args)

	// Grab the list of flags from the Flag object
	flags := make([]string, 0)
	cmd.set.VisitSets(func(name string, set *cmnFlag.Set) {
		set.VisitAll(func(flag *flag.Flag) {
			flags = append(flags, fmt.Sprintf("-%s", flag.Name))
		})
	})

	// Verify that there is a prediction for each flag associated with the command
	assert.Equal(t, len(flags), len(res))
	assert.ElementsMatch(t, flags, res, "flags and predictions didn't match, make sure to add "+
		"new flags to the command AutoCompleteFlags function")
}

func TestTaskCreateCommand_AutocompleteArgs(t *testing.T) {
	cmd := getInitializedCommand(t)
	c := cmd.AutocompleteArgs()
	assert.Equal(t, complete.PredictNothing, c)
}
