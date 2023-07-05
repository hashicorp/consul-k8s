//go:build linux
// +build linux

package ebpf

import (
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/go-logr/logr"
	"os"
	"path"
	"time"
)

//go:generate bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" bpf cgroup_connect4.c -- -I./headers

const bpfFSPath = " /consul-ebf/fs/bpf"
const sysGroupFSPath = "/consul-ebf/fs/cgroup"

type BpfProgram struct {
	objs   bpfObjects
	logger logr.Logger
	l      link.Link
}

func New(logger logr.Logger) *BpfProgram {
	return &BpfProgram{logger: logger}
}

func (p *BpfProgram) LoadBpfProgram() error {
	// Name of the kernel function we're tracing
	fn := "consul_bpf"
	//if err := rlimit.RemoveMemlock(); err != nil {
	//	p.logger.Error(err, "memlock error")
	//	return err
	//}
	//if err := os.MkdirAll("/consul", 0777); err != nil {
	//	p.logger.Error(err, "failed to create consul fs subpath", "path", "/consul")
	//	return err
	//}
	pinPath := path.Join(bpfFSPath, fn)
	if err := os.MkdirAll(pinPath, 0777); err != nil {
		p.logger.Error(err, "failed to create bpf fs subpath", "path", pinPath)
		return err
	}

	// Load pre-compiled programs and maps into the kernel.
	p.objs = bpfObjects{}
	if err := loadBpfObjects(&p.objs, &ebpf.CollectionOptions{
		Maps: ebpf.MapOptions{
			// Pin the map to the BPF filesystem and configure the
			// library to automatically re-write it in the BPF
			// program so it can be re-used if it already exists or
			// create it if not
			PinPath: pinPath,
		},
	}); err != nil {
		p.logger.Error(err, "loading objects")
		return err
	}
	info, err := p.objs.bpfMaps.V4SvcMap.Info()
	if err != nil {
		p.logger.Error(err, "Cannot get map info")
		return err
	}
	p.logger.Info("eBPF Program successfully loaded ", "info", info)

	time.Sleep(1 * time.Hour)
	// Link the proxy program to the default cgroup.
	p.l, err = link.AttachCgroup(link.CgroupOptions{
		Path:    sysGroupFSPath,
		Attach:  ebpf.AttachCGroupInet4Connect,
		Program: p.objs.Sock4Connect,
	})
	if err != nil {
		p.logger.Error(err, "Attach failed")
		return err
	}

	return nil
}

func (p *BpfProgram) UnloadBpfProgram() error {
	p.logger.Info("Cleaning up eBPF program")
	err := p.objs.Close()
	if err != nil {
		return err
	}
	return p.l.Close()
}
