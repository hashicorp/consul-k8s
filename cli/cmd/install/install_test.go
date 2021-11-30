package install

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/cmd/common"
	"github.com/hashicorp/go-hclog"
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
	require.Contains(t, err.Error(), "found PVCs from previous installations (default/consul-server-test1,default/consul-server-test2), delete before re-installing")

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
	c := getInitializedCommand(t)
	c.kubernetes = fake.NewSimpleClientset()
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-consul-bootstrap-acl-token",
			Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
		},
	}
	c.kubernetes.CoreV1().Secrets("default").Create(context.Background(), secret, metav1.CreateOptions{})
	err := c.checkForPreviousSecrets()
	require.Error(t, err)
	require.Contains(t, err.Error(), "found Consul secret from previous installation")

	// Clear out the client and make sure the check now passes.
	c.kubernetes = fake.NewSimpleClientset()
	err = c.checkForPreviousSecrets()
	require.NoError(t, err)

	// Add a new irrelevant secret and make sure the check continues to pass.
	secret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "irrelevant-secret",
		},
	}
	c.kubernetes.CoreV1().Secrets("default").Create(context.Background(), secret, metav1.CreateOptions{})
	err = c.checkForPreviousSecrets()
	require.NoError(t, err)
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

// TestValidLabel calls validLabel() which checks strings match RFC 1123 label convention.
func TestValidLabel(t *testing.T) {
	testCases := []struct {
		description string
		input       string
		expected    bool
	}{
		{
			"Standard name with leading numbers works.",
			"1234-abc",
			true,
		},
		{
			"All lower case letters works.",
			"peppertrout",
			true,
		},
		{
			"Test that dashes in the middle are allowed.",
			"pepper-trout",
			true,
		},
		{
			"Capitals violate RFC 1123 lower case label.",
			"Peppertrout",
			false,
		},
		{
			"Underscores are not permitted anywhere.",
			"ab_cd",
			false,
		},
		{
			"The dash must be in the middle of the word, not on the start/end character.",
			"peppertrout-",
			false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			if result := validLabel(testCase.input); result != testCase.expected {
				t.Errorf("Incorrect output, got %v and expected %v", result, testCase.expected)
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

	// Enterprise secret and image are valid.
	c.kubernetes.CoreV1().Secrets("consul").Create(context.Background(), secret, metav1.CreateOptions{})
	err := c.checkValidEnterprise(secret.Name, "consul-enterprise:-ent")
	require.NoError(t, err)

	// Enterprise secret provided but not an enterprise image.
	err = c.checkValidEnterprise(secret.Name, "consul:")
	require.Error(t, err)
	require.Contains(t, err.Error(), "enterprise Consul image is not provided")

	// Enterprise secret does not exist.
	err = c.checkValidEnterprise("consul-unrelated-secret", "consul-enterprise:-ent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "please make sure that the secret exists")

	// Enterprise secret exists in a different namespace.
	c.kubernetes.CoreV1().Secrets("unrelated").Create(context.Background(), secret2, metav1.CreateOptions{})
	err = c.checkValidEnterprise(secret2.Name, "consul-enterprise:-ent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "please make sure that the secret exists")
}
