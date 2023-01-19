package log

import (
	"errors"
	"strings"
	"sync"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/flag"
)

type LogCommand struct {
	*common.BaseCommand
	set *flag.Sets

	// Command Flags
	podName string

	once sync.Once
	help string
}

var ErrMissingPodName = errors.New("Exactly one positional argument is requied: <pod-name>")

func (l *LogCommand) init() {
	l.set = flag.NewSets()
}

func (l *LogCommand) Run(args []string) int {
	l.once.Do(l.init)
	l.Log.ResetNamed("log")
	defer common.CloseWithError(l.BaseCommand)

	err := l.parseFlags(args)
	if err != nil {
		return 1
	}
	return 0
}

func (l *LogCommand) parseFlags(args []string) error {
	positional := []string{}
	// Separate positional args from keyed args
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		positional = append(positional, arg)
	}
	keyed := args[len(positional):]

	if len(positional) != 1 {
		return ErrMissingPodName
	}

	l.podName = positional[0]

	err := l.set.Parse(keyed)
	if err != nil {
		return err
	}
	return nil
}
