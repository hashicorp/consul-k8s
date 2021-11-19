package aclinit

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Test that we write the secret data to a file.
func TestRun_TokenSinkFile(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	// Set up k8s with the secret.
	token := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	k8sNS := "default"
	secretName := "secret-name"
	k8s := fake.NewSimpleClientset()
	_, err = k8s.CoreV1().Secrets(k8sNS).Create(
		context.Background(),
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   secretName,
				Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
			},
			Data: map[string][]byte{
				"token": []byte(token),
			},
		},
		metav1.CreateOptions{})

	require.NoError(err)

	sinkFile := filepath.Join(tmpDir, "acl-token")
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	code := cmd.Run([]string{
		"-k8s-namespace", k8sNS,
		"-token-sink-file", sinkFile,
		"-secret-name", secretName,
	})
	require.Equal(0, code, ui.ErrorWriter.String())
	bytes, err := ioutil.ReadFile(sinkFile)
	require.NoError(err)
	require.Equal(token, string(bytes), "exp: %s, got: %s", token, string(bytes))
}

// Test that if there's an error writing the sink file it's returned.
func TestRun_TokenSinkFileErr(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	// Set up k8s with the secret.
	token := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	k8sNS := "default"
	secretName := "secret-name"
	k8s := fake.NewSimpleClientset()
	_, err := k8s.CoreV1().Secrets(k8sNS).Create(
		context.Background(),
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   secretName,
				Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
			},
			Data: map[string][]byte{
				"token": []byte(token),
			},
		},
		metav1.CreateOptions{})

	require.NoError(err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	code := cmd.Run([]string{
		"-k8s-namespace", k8sNS,
		"-token-sink-file", "/this/filepath/does/not/exist",
		"-secret-name", secretName,
	})

	require.Equal(1, code)
	require.Contains(ui.ErrorWriter.String(),
		`Error writing token to file "/this/filepath/does/not/exist": open /this/filepath/does/not/exist: no such file or directory`,
	)
}

// Test that if the command is run twice it succeeds. This test is the result
// of a bug that we discovered where the command failed on subsequent runs because
// the token file only had read permissions (0400).
func TestRun_TokenSinkFileTwice(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	// Set up k8s with the secret.
	token := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	k8sNS := "default"
	secretName := "secret-name"
	k8s := fake.NewSimpleClientset()
	_, err = k8s.CoreV1().Secrets(k8sNS).Create(
		context.Background(),
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   secretName,
				Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
			},
			Data: map[string][]byte{
				"token": []byte(token),
			},
		},
		metav1.CreateOptions{})

	sinkFile := filepath.Join(tmpDir, "acl-token")
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}

	require.NoError(err)

	// Run twice.
	for i := 0; i < 2; i++ {
		code := cmd.Run([]string{
			"-k8s-namespace", k8sNS,
			"-token-sink-file", sinkFile,
			"-secret-name", secretName,
		})
		require.Equal(0, code, ui.ErrorWriter.String())

		bytes, err := ioutil.ReadFile(sinkFile)
		require.NoError(err)
		require.Equal(token, string(bytes), "exp: %s, got: %s", token, string(bytes))
	}
}
