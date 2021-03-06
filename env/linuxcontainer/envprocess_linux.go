package linuxcontainer

import (
	"time"

	"github.com/criyle/go-judge/envexec"
	"github.com/criyle/go-sandbox/runner"
)

var _ envexec.Process = &process{}

// process defines the running process
type process struct {
	rt   runner.Result
	done chan struct{}
	cg   Cgroup
}

func newProcess(ch <-chan runner.Result, cg Cgroup, cgPool CgroupPool) *process {
	p := &process{
		done: make(chan struct{}),
		cg:   cg,
	}
	go func() {
		defer close(p.done)
		if cgPool != nil {
			defer cgPool.Put(cg)
		}
		p.rt = <-ch
		if cg != nil {
			if t, err := cg.CPUUsage(); err == nil {
				p.rt.Time = t
			}
			if m, err := cg.MemoryUsage(); err == nil {
				p.rt.Memory = m
			}
		}
	}()
	return p
}

func (p *process) Done() <-chan struct{} {
	return p.done
}

func (p *process) Result() envexec.RunnerResult {
	<-p.done
	return p.rt
}

func (p *process) Usage() envexec.Usage {
	var (
		t time.Duration
		m envexec.Size
	)
	if p.cg != nil {
		t, _ = p.cg.CPUUsage()
		m, _ = p.cg.MemoryUsage()
	}
	return envexec.Usage{
		Time:   t,
		Memory: m,
	}
}
