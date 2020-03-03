package runner

import (
	"sync"
	"time"

	"github.com/criyle/go-judge/pkg/envexec"
	"github.com/criyle/go-sandbox/container"
	"github.com/criyle/go-sandbox/pkg/cgroup"
	"github.com/criyle/go-sandbox/runner"
)

type pool struct {
	builder EnvironmentBuilder

	env []container.Environment
	mu  sync.Mutex
}

func newPool(builder EnvironmentBuilder) *pool {
	return &pool{
		builder: builder,
	}
}

func (p *pool) Get() (container.Environment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.env) > 0 {
		rt := p.env[len(p.env)-1]
		p.env = p.env[:len(p.env)-1]
		return rt, nil
	}
	return p.builder.Build()
}

func (p *pool) Put(env container.Environment) {
	env.Reset()

	p.mu.Lock()
	defer p.mu.Unlock()

	p.env = append(p.env, env)
}

func (p *pool) Destroy(env container.Environment) {
	env.Destroy()
}

func (p *pool) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, e := range p.env {
		p.Destroy(e)
	}
}

func (p *pool) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, e := range p.env {
		p.Destroy(e)
	}
}

type wCgroup cgroup.Cgroup

func (c *wCgroup) SetMemoryLimit(s runner.Size) error {
	return (*cgroup.Cgroup)(c).SetMemoryLimitInBytes(uint64(s))
}

func (c *wCgroup) SetProcLimit(l uint64) error {
	return (*cgroup.Cgroup)(c).SetPidsMax(l)
}

func (c *wCgroup) CPUUsage() (time.Duration, error) {
	t, err := (*cgroup.Cgroup)(c).CpuacctUsage()
	return time.Duration(t), err
}

func (c *wCgroup) MemoryUsage() (runner.Size, error) {
	s, err := (*cgroup.Cgroup)(c).MemoryMaxUsageInBytes()
	if err != nil {
		return 0, err
	}
	return runner.Size(s), nil
	// not really useful if creates new
	// cache, err := (*cgroup.CGroup)(c).FindMemoryStatProperty("cache")
	// if err != nil {
	// 	return 0, err
	// }
	// return runner.Size(s - cache), err
}

func (c *wCgroup) AddProc(pid int) error {
	return (*cgroup.Cgroup)(c).AddProc(pid)
}

func (c *wCgroup) Reset() error {
	if err := (*cgroup.Cgroup)(c).SetCpuacctUsage(0); err != nil {
		return err
	}
	if err := (*cgroup.Cgroup)(c).SetMemoryMaxUsageInBytes(0); err != nil {
		return err
	}
	return nil
}

func (c *wCgroup) Destory() error {
	return (*cgroup.Cgroup)(c).Destroy()
}

type fCgroupPool struct {
	builder CgroupBuilder
}

func newFakeCgroupPool(builder CgroupBuilder) *fCgroupPool {
	return &fCgroupPool{builder: builder}
}

func (f *fCgroupPool) Get() (envexec.Cgroup, error) {
	cg, err := f.builder.Build()
	if err != nil {
		return nil, err
	}
	return (*wCgroup)(cg), nil
}

func (f *fCgroupPool) Put(c envexec.Cgroup) {
	c.Destory()
}

func (f *fCgroupPool) Shutdown() {

}

type wCgroupPool struct {
	builder CgroupBuilder

	cgs []envexec.Cgroup
	mu  sync.Mutex
}

func newCgroupPool(builder CgroupBuilder) *wCgroupPool {
	return &wCgroupPool{builder: builder}
}

func (w *wCgroupPool) Get() (envexec.Cgroup, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.cgs) > 0 {
		rt := w.cgs[len(w.cgs)-1]
		w.cgs = w.cgs[:len(w.cgs)-1]
		return rt, nil
	}

	cg, err := w.builder.Build()
	if err != nil {
		return nil, err
	}
	return (*wCgroup)(cg), nil
}

func (w *wCgroupPool) Put(c envexec.Cgroup) {
	w.mu.Lock()
	defer w.mu.Unlock()

	c.Reset()
	w.cgs = append(w.cgs, c)
}

func (w *wCgroupPool) Shutdown() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, c := range w.cgs {
		c.Destory()
	}
}
