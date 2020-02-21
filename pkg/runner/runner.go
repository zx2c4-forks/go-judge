package runner

import (
	"context"
	"time"

	"github.com/criyle/go-judge/file"
	"github.com/criyle/go-judge/types"
	"github.com/criyle/go-sandbox/container"
	"github.com/criyle/go-sandbox/pkg/cgroup"
	stypes "github.com/criyle/go-sandbox/types"
)

// Pool implements pool of environments
type Pool interface {
	Get() (container.Environment, error)
	Put(container.Environment)
}

// PipeCollector can be used in Cmd.Files paramenter
type PipeCollector struct {
	Name      string
	SizeLimit int64
}

// PipeIndex defines the index of cmd and the fd of the that cmd
type PipeIndex struct {
	Index int
	Fd    int
}

// Pipe defines the pipe between parallel Cmd
type Pipe struct {
	In, Out PipeIndex
}

// CPUUsager access process cpu usage (from cgroup)
type CPUUsager interface {
	CPUUsage() (time.Duration, error)
}

// Cmd defines instruction to run a program in container environment
type Cmd struct {
	// argument, environment
	Args []string
	Env  []string

	// fds for exec: can be nil, file.Opener, PipeCollector
	// nil: undefined, will be closed
	// *os.File: will be fd and passed to runner, file will be close after cmd starts
	// file.Opener: will be opened and passed to runner
	// PipeCollector: a pipe write end will be passed to runner and collected as a copyout file
	Files []interface{}

	// cgroup limits
	MemoryLimit stypes.Size // in bytes
	PidLimit    uint64

	// file contents to copyin before exec
	CopyIn map[string]file.File

	// file names to copyout after exec
	CopyOut []string

	// Waiter is called after cmd starts and it should return
	// once time limit exceeded.
	// return true to as TLE and false as normal exits (context finished)
	Waiter func(context.Context, CPUUsager) bool
}

// CgroupBuilder builds cgroup for runner
type CgroupBuilder interface {
	Build() (cg *cgroup.CGroup, err error)
}

// Runner defines the running instruction to run multiple
// Exec in parallel restricted within cgroup
type Runner struct {
	// CGBuilder defines cgroup builder used for Cmd
	// must have cpu, memory and pids sub-cgroup
	CGBuilder CgroupBuilder

	// EnvironmentPool defines pool used for runner environment
	EnvironmentPool Pool

	// Cmds defines Cmd running in parallel
	Cmds []*Cmd

	// Pipes defines the potential mapping between Cmds.
	// ensure nil is used as placeholder in Cmd
	Pipes []*Pipe
}

// Result defines the running result for single Cmd
type Result struct {
	Status types.Status

	Error string // error

	Time   time.Duration
	Memory stypes.Size // byte

	// Files stores copy out files
	Files map[string]file.File
}