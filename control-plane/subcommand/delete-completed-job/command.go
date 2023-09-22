// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package deletecompletedjob

import (
	"context"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/mitchellh/cli"
	v1 "k8s.io/api/batch/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/flags"
)

// Command is the command for deleting completed jobs.
type Command struct {
	UI cli.Ui

	flags         *flag.FlagSet
	k8s           *flags.K8SFlags
	flagNamespace string
	flagTimeout   string
	flagLogLevel  string
	flagLogJSON   bool

	once      sync.Once
	help      string
	k8sClient kubernetes.Interface

	// retryDuration is how often we'll retry deletion.
	retryDuration time.Duration

	ctx context.Context
}

func (c *Command) init() {
	c.flags = flag.NewFlagSet("", flag.ContinueOnError)

	c.k8s = &flags.K8SFlags{}
	c.flags.StringVar(&c.flagNamespace, "k8s-namespace", "",
		"Name of Kubernetes namespace where the job is deployed")
	c.flags.StringVar(&c.flagTimeout, "timeout", "30m",
		"How long we'll wait for the job to complete before timing out, e.g. 1ms, 2s, 3m")
	c.flags.StringVar(&c.flagLogLevel, "log-level", "info",
		"Log verbosity level. Supported values (in order of detail) are \"trace\", "+
			"\"debug\", \"info\", \"warn\", and \"error\".")
	c.flags.BoolVar(&c.flagLogJSON, "log-json", false,
		"Enable or disable JSON output format for logging.")
	flags.Merge(c.flags, c.k8s.Flags())
	c.help = flags.Usage(help, c.flags)

	// Default retry to 1s. This is exposed for setting in tests.
	if c.retryDuration == 0 {
		c.retryDuration = 1 * time.Second
	}
}

// Run will attempt to delete the job once it succeeds. If the job hits its
// backoff limit, it will give up deleting it.
func (c *Command) Run(args []string) int {
	c.once.Do(c.init)

	// Validate command.
	if err := c.flags.Parse(args); err != nil {
		return 1
	}
	if len(c.flags.Args()) != 1 {
		c.UI.Error("Must have one arg: the job name to delete.")
		return 1
	}
	jobName := c.flags.Args()[0]
	if c.flagNamespace == "" {
		c.UI.Error("Must set flag -k8s-namespace")
		return 1
	}
	timeout, err := time.ParseDuration(c.flagTimeout)
	if err != nil {
		c.UI.Error(fmt.Sprintf("%q is not a valid timeout: %s", c.flagTimeout, err))
		return 1
	}

	if c.ctx == nil {
		var cancel context.CancelFunc
		c.ctx, cancel = context.WithTimeout(context.Background(), timeout)
		// The context will only ever be intentionally ended by the timeout.
		defer cancel()
	}

	// c.k8sclient might already be set in a test.
	if c.k8sClient == nil {
		config, err := subcommand.K8SConfig(c.k8s.KubeConfig())
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error retrieving Kubernetes auth: %s", err))
			return 1
		}

		c.k8sClient, err = kubernetes.NewForConfig(config)
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error initializing Kubernetes client: %s", err))
			return 1
		}
	}

	logger, err := common.Logger(c.flagLogLevel, c.flagLogJSON)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	// Wait for job to complete.
	logger.Info(fmt.Sprintf("waiting for job %q to complete successfully", jobName))
	for {
		job, err := c.k8sClient.BatchV1().Jobs(c.flagNamespace).Get(c.ctx, jobName, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			logger.Info(fmt.Sprintf("job %q does not exist, no need to delete", jobName))
			return 0
		}
		if err != nil {
			c.UI.Error(fmt.Sprintf("Error getting job %q: %s", jobName, err))
			return 1
		}

		// If its succeeded we're done.
		if job.Status.Succeeded > 0 {
			break
		}

		// If its reached its backoff limit then it will never complete.
		for _, condition := range job.Status.Conditions {
			if condition.Type == v1.JobFailed && condition.Reason == "BackoffLimitExceeded" {
				logger.Warn(fmt.Sprintf("job %q has reached its backoff limit and will never complete", jobName))
				return 1
			}
		}

		logger.Info(fmt.Sprintf("job %q has not yet succeeded, waiting %v", jobName, c.retryDuration))
		// Wait on either the retry duration (in which case we continue) or the
		// overall command timeout.
		select {
		case <-time.After(c.retryDuration):
			continue
		case <-c.ctx.Done():
			logger.Warn(fmt.Sprintf("timeout %q has been reached, exiting without deleting job", timeout))
			return 1
		}
	}

	// Here we know the job has succeeded. We can delete it and then delete
	// ourselves.
	logger.Info(fmt.Sprintf("job %q has succeeded, deleting", jobName))
	propagationPolicy := metav1.DeletePropagationForeground
	err = c.k8sClient.BatchV1().Jobs(c.flagNamespace).Delete(c.ctx, jobName, metav1.DeleteOptions{
		// Needed so that the underlying pods are also deleted.
		PropagationPolicy: &propagationPolicy,
	})
	if err != nil {
		c.UI.Error(fmt.Sprintf("unable to delete job %q: %s", jobName, err))
		return 1
	}

	logger.Info(fmt.Sprintf("Deleted job %q successfully", jobName))
	return 0
}

func (c *Command) Synopsis() string { return synopsis }
func (c *Command) Help() string {
	c.once.Do(c.init)
	return c.help
}

const synopsis = "Delete Kubernetes Job when complete."
const help = `
Usage: consul-k8s-control-plane delete-completed-job [name] [options]

  Waits for job to complete, then deletes it. If the job reaches its
  backoff limit then the command will exit.
`
