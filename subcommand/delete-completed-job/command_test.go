package deletecompletedjob

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	batch "k8s.io/api/batch/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun_ArgValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		args   []string
		expErr string
	}{
		{
			[]string{},
			"Must have one arg: the job name to delete.",
		},
		{
			[]string{"job-name"},
			"Must set flag -k8s-namespace",
		},
		{
			[]string{"-k8s-namespace=", "job-name"},
			"Must set flag -k8s-namespace",
		},
		{
			[]string{"-k8s-namespace=default", "-timeout=10jd", "job-name"},
			"\"10jd\" is not a valid timeout",
		},
	}
	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			k8s := fake.NewSimpleClientset()
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				k8sClient: k8s,
			}
			cmd.init()
			responseCode := cmd.Run(c.args)
			require.Equal(t, 1, responseCode)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

func TestRun_JobDoesNotExist(t *testing.T) {
	t.Parallel()
	ns := "default"
	jobName := "job"
	k8s := fake.NewSimpleClientset()
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	cmd.init()

	responseCode := cmd.Run([]string{
		"-k8s-namespace", ns,
		jobName,
	})
	require.Equal(t, 0, responseCode, ui.ErrorWriter.String())
}

// Test when the job condition changes to either success or failed.
func TestRun_JobConditionChanges(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		EventualStatus batch.JobStatus
		ExpDelete      bool
		ExpCode        int
	}{
		"job fails": {
			EventualStatus: batch.JobStatus{
				Active: 0,
				Failed: 1,
				Conditions: []batch.JobCondition{
					{
						Type:    batch.JobFailed,
						Status:  "True",
						Reason:  "BackoffLimitExceeded",
						Message: "Job has reached the specified backoff limit",
					},
				},
			},
			ExpDelete: false,
			ExpCode:   1,
		},
		"job succeeds": {
			EventualStatus: batch.JobStatus{
				Succeeded: 1,
				Conditions: []batch.JobCondition{
					{
						Type:   batch.JobComplete,
						Status: "True",
					},
				},
			},
			ExpDelete: true,
			ExpCode:   0,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ns := "default"
			jobName := "job"
			k8s := fake.NewSimpleClientset()
			require := require.New(t)

			// Create the job that's not complete.
			_, err := k8s.BatchV1().Jobs(ns).Create(
				context.Background(),
				&batch.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name: jobName,
					},
					Status: batch.JobStatus{
						Active: 1,
					},
				},
				metav1.CreateOptions{})
			require.NoError(err)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				k8sClient: k8s,
				// Set a low retry for tests.
				retryDuration: 20 * time.Millisecond,
			}
			cmd.init()

			// Start the command before the Pod exist.
			// Run in a goroutine so we can create the Pods asynchronously
			done := make(chan bool)
			var responseCode int
			go func() {
				responseCode = cmd.Run([]string{
					"-k8s-namespace", ns,
					jobName,
				})
				close(done)
			}()

			// Asynchronously update the job to be complete.
			go func() {
				// Update after a delay between 100 and 500ms.
				// It's randomized to ensure we're not relying on specific timing.
				delay := 100 + rand.Intn(400)
				time.Sleep(time.Duration(delay) * time.Millisecond)

				_, err := k8s.BatchV1().Jobs(ns).Update(
					context.Background(),
					&batch.Job{
						ObjectMeta: metav1.ObjectMeta{
							Name: jobName,
						},
						Status: c.EventualStatus,
					},
					metav1.UpdateOptions{})
				require.NoError(err)
			}()

			// Wait for the command to exit.
			select {
			case <-done:
				require.Equal(c.ExpCode, responseCode, ui.ErrorWriter.String())
			case <-time.After(2 * time.Second):
				require.FailNow("command did not exit after 2s")
			}

			// Check job deletion.
			_, err = k8s.BatchV1().Jobs(ns).Get(context.Background(), jobName, metav1.GetOptions{})
			if c.ExpDelete {
				require.True(k8serrors.IsNotFound(err))
			} else {
				require.NoError(err)
			}
		})
	}
}

// Test that the job times out after a certain duration.
func TestRun_Timeout(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	ns := "default"
	jobName := "job"
	k8s := fake.NewSimpleClientset()

	// Create the job that's not complete.
	_, err := k8s.BatchV1().Jobs(ns).Create(
		context.Background(),
		&batch.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name: jobName,
			},
			Status: batch.JobStatus{
				Active: 1,
			},
		},
		metav1.CreateOptions{})
	require.NoError(err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:            ui,
		k8sClient:     k8s,
		retryDuration: 100 * time.Millisecond,
	}
	cmd.init()

	done := make(chan bool)
	var responseCode int
	go func() {
		responseCode = cmd.Run([]string{
			"-k8s-namespace", ns,
			"-timeout=1s",
			jobName,
		})
		close(done)
	}()

	// Wait for the command to exit.
	select {
	case <-done:
		require.Equal(1, responseCode, ui.ErrorWriter.String())
	case <-time.After(2 * time.Second):
		require.FailNow("command did not exit after 2s")
	}

	// The job should not have been deleted.
	_, err = k8s.BatchV1().Jobs(ns).Get(context.Background(), jobName, metav1.GetOptions{})
	require.NoError(err)
}
