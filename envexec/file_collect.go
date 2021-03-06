package envexec

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/criyle/go-judge/file"
	"github.com/criyle/go-sandbox/runner"
	"golang.org/x/sync/errgroup"
)

// copyOutAndCollect reads file and pipes in parallel from container
func copyOutAndCollect(m Environment, c *Cmd, ptc []pipeCollector) (map[string]file.File, error) {
	var (
		g errgroup.Group
		l sync.Mutex
	)
	rt := make(map[string]file.File)
	put := func(f file.File) {
		l.Lock()
		defer l.Unlock()
		rt[f.Name()] = f
	}

	// copy out
	for _, n := range c.CopyOut {
		n := n
		g.Go(func() error {
			cf, err := m.Open(n, os.O_RDONLY, 0777)
			if err != nil {
				return err
			}
			defer cf.Close()

			stat, err := cf.Stat()
			if err != nil {
				return err
			}
			// check regular file
			if stat.Mode()&os.ModeType != 0 {
				return fmt.Errorf("File(%s) is not a regular file %d", n, stat.Mode()&os.ModeType)
			}
			// check size limit
			s := stat.Size()
			if c.CopyOutMax > 0 && s > int64(c.CopyOutMax) {
				return fmt.Errorf("File(%s) have size (%d) exceeded the limit (%d)", n, s, c.CopyOutMax)
			}
			var buf bytes.Buffer
			buf.Grow(int(s))

			// Ensure not copy over file size
			_, err = buf.ReadFrom(io.LimitReader(cf, s))
			if err != nil {
				return err
			}
			put(file.NewMemFile(n, buf.Bytes()))
			return nil
		})
	}

	// collect pipe
	for _, p := range ptc {
		p := p
		g.Go(func() error {
			<-p.buff.Done
			if int64(p.buff.Buffer.Len()) > p.buff.Max {
				return runner.StatusOutputLimitExceeded
			}
			put(file.NewMemFile(p.name, p.buff.Buffer.Bytes()))
			return nil
		})
	}

	// copy out dir
	if c.CopyOutDir != "" {
		g.Go(func() error {
			return copyDir(m.WorkDir(), c.CopyOutDir)
		})
	}

	err := g.Wait()
	return rt, err
}
