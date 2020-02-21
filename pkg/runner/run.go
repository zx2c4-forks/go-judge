package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/criyle/go-judge/file"
	"github.com/criyle/go-judge/types"
	"github.com/criyle/go-sandbox/container"
	"github.com/criyle/go-sandbox/pkg/cgroup"
	"github.com/criyle/go-sandbox/pkg/pipe"
	stypes "github.com/criyle/go-sandbox/types"
)

var memoryLimitExtra uint64 = 16 << 10 // 16k more memory

type pipeBuff struct {
	buff *pipe.Buffer
	name string
}

type cgroupCPUUsager struct {
	*cgroup.CGroup
}

func (c cgroupCPUUsager) CPUUsage() (time.Duration, error) {
	u, err := c.CpuacctUsage()
	return time.Duration(u), err
}

// Run starts the cmd and returns results
func (r *Runner) Run() ([]Result, error) {
	var (
		fileToClose []*os.File
	)

	defer func() {
		closeFiles(fileToClose)
	}()

	// prepare files
	fds, pipeToCollect, fileToClose, err := prepareFds(r)
	if err != nil {
		return nil, err
	}

	// prepare environments
	ms := make([]container.Environment, 0, len(r.Cmds))
	for range r.Cmds {
		m, err := r.EnvironmentPool.Get()
		if err != nil {
			return nil, fmt.Errorf("failed to get environment %v", err)
		}
		defer r.EnvironmentPool.Put(m)
		ms = append(ms, m)
	}

	// prepare cgroup
	cgs := make([]*cgroup.CGroup, 0, len(r.Cmds))
	for range r.Cmds {
		cg, err := r.CGBuilder.Build()
		if err != nil {
			return nil, fmt.Errorf("failed to build cgroup %v", err)
		}
		defer cg.Destroy()
		cgs = append(cgs, cg)
	}

	// run cmds
	var wg sync.WaitGroup
	wg.Add(len(r.Cmds))

	result := make([]Result, len(r.Cmds))
	errC := make(chan error, 1)

	for i, c := range r.Cmds {
		go func(i int, c *Cmd) {
			defer wg.Done()
			r, err := runOne(ms[i], cgs[i], c, fds[i], pipeToCollect[i])
			if err != nil {
				writeErrorC(errC, err)
				return
			}
			result[i] = <-r
		}(i, c)
	}
	wg.Wait()
	fileToClose = nil // already closed by runOne

	// collect potential error
	select {
	case err = <-errC:
	default:
	}
	return result, err
}

// fds will be closed
func runOne(m container.Environment, cg *cgroup.CGroup, c *Cmd, fds []*os.File, ptc []pipeBuff) (<-chan Result, error) {
	fdToClose := fds
	defer func() {
		closeFiles(fdToClose)
	}()

	// setup cgroup limits
	if err := cg.SetMemoryLimitInBytes(uint64(c.MemoryLimit) + memoryLimitExtra); err != nil {
		return nil, err
	}
	if err := cg.SetPidsMax(c.PidLimit); err != nil {
		return nil, err
	}

	// copyin
	if len(c.CopyIn) > 0 {
		if err := copyIn(m, c.CopyIn); err != nil {
			return nil, err
		}
	}

	// set running parameters
	execParam := container.ExecveParam{
		Args:     c.Args,
		Env:      c.Env,
		Fds:      getFdArray(fds),
		SyncFunc: cg.AddProc,
	}

	// start the cmd (they will be canceled in other goroutines)
	ctx, cancel := context.WithCancel(context.TODO())
	waiterCtx, waiterCancel := context.WithCancel(ctx)

	rc := m.Execve(ctx, execParam)

	// close files
	closeFiles(fds)
	fdToClose = nil

	// results
	result := make(chan Result, 1)

	// wait to finish
	// 1. cmd exit first, signal waiter to exit
	// 2. waiter exit first, signal proc to exit
	var tle bool
	go func() {
		tle = c.Waiter(waiterCtx, cgroupCPUUsager{cg})
		cancel()
	}()

	go func() {
		rt := <-rc
		waiterCancel()
		// collect result
		files, err := copyOutAndCollect(m, c, ptc)
		re := Result{
			Status: convertStatus(rt.Status),
			Error:  rt.Error,
			Time:   rt.Time,
			Memory: rt.Memory,
			Files:  files,
		}
		// collect error
		if err != nil && re.Error == "" {
			switch err := err.(type) {
			case stypes.Status:
				re.Status = convertStatus(err)
			default:
				re.Status = types.StatusInternalError
			}
			re.Error = err.Error()
		}
		// time
		cpuUsage, err := cg.CpuacctUsage()
		if err != nil {
			re.Status = types.StatusInternalError
			re.Error = err.Error()
		} else {
			re.Time = time.Duration(cpuUsage)
		}
		// memory
		memoryUsage, err := cg.MemoryMaxUsageInBytes()
		if err != nil {
			re.Status = types.StatusInternalError
			re.Error = err.Error()
		} else {
			re.Memory = stypes.Size(memoryUsage)
		}
		// wait waiter done
		<-ctx.Done()
		if tle {
			re.Status = types.StatusTimeLimitExceeded
		}
		if re.Memory > c.MemoryLimit {
			re.Status = types.StatusMemoryLimitExceeded
		}
		result <- re
	}()

	return result, nil
}

func copyIn(m container.Environment, copyIn map[string]file.File) error {
	// open copyin files
	openCmd := make([]container.OpenCmd, 0, len(copyIn))
	files := make([]file.File, 0, len(copyIn))
	for n, f := range copyIn {
		openCmd = append(openCmd, container.OpenCmd{
			Path: n,
			Flag: os.O_CREATE | os.O_RDWR | os.O_TRUNC,
			Perm: 0777,
		})
		files = append(files, f)
	}

	// open files from container
	cFiles, err := m.Open(openCmd)
	if err != nil {
		return err
	}

	// copyin in parallel
	var wg sync.WaitGroup
	errC := make(chan error, 1)
	wg.Add(len(files))
	for i, f := range files {
		go func(cFile *os.File, hFile file.File) {
			defer wg.Done()
			defer cFile.Close()

			// open host file
			hf, err := hFile.Open()
			if err != nil {
				writeErrorC(errC, err)
				return
			}
			defer hf.Close()

			// copy to container
			_, err = io.Copy(cFile, hf)
			if err != nil {
				writeErrorC(errC, err)
				return
			}
		}(cFiles[i], f)
	}
	wg.Wait()

	// check error
	select {
	case err := <-errC:
		return err
	default:
	}
	return nil
}

func convertStatus(s stypes.Status) types.Status {
	switch s {
	case stypes.StatusNormal:
		return types.StatusAccepted
	case stypes.StatusSignalled, stypes.StatusNonzeroExitStatus:
		return types.StatusRuntimeError
	case stypes.StatusMemoryLimitExceeded:
		return types.StatusMemoryLimitExceeded
	case stypes.StatusTimeLimitExceeded:
		return types.StatusTimeLimitExceeded
	case stypes.StatusOutputLimitExceeded:
		return types.StatusOutputLimitExceeded
	case stypes.StatusDisallowedSyscall:
		return types.StatusDangerousSyscall
	default:
		return types.StatusInternalError
	}
}

func getFdArray(fd []*os.File) []uintptr {
	r := make([]uintptr, 0, len(fd))
	for _, f := range fd {
		r = append(r, f.Fd())
	}
	return r
}

func closeFiles(files []*os.File) {
	for _, f := range files {
		f.Close()
	}
}

func writeErrorC(errC chan error, err error) {
	select {
	case errC <- err:
	default:
	}
}