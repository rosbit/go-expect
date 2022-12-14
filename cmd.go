package expect

import (
	"github.com/creack/pty"
	"golang.org/x/term"
	"os/exec"
	"fmt"
	"io"
	"os"
	"syscall"
)

type IOHandlers interface {
	HandleStdin(io.WriteCloser)
	HandleStdout(io.ReadCloser)
	HandleStderr(io.ReadCloser)
	GetEnvs() map[string]string
}

type Cmd struct {
	cmd *exec.Cmd
	Exit chan struct{}
	master, slave *os.File
	waitErr error
}

type fnPopen func(ioHandlers IOHandlers, cmdPath string, arg ...string) (cmd *Cmd, err error)

func Popen(ioHandlers IOHandlers, cmdPath string, arg ...string) (cmd *Cmd, err error) {
	c := exec.Command(cmdPath, arg...)
	setEnvs(c, ioHandlers)

	stdin, e := c.StdinPipe()
	if e != nil {
		err = e
		return
	}
	stdout, e := c.StdoutPipe()
	if e != nil {
		err = e
		return
	}
	if s, ok := stdout.(*os.File); ok {
		makeRaw(s, true)
	}
	stderr, e := c.StderrPipe()
	if e != nil {
		err = e
		return
	}

	if err = c.Start(); err != nil {
		return
	}

	cmd = &Cmd{
		cmd: c,
		Exit: make(chan struct{}),
	}

	if ioHandlers != nil {
		go ioHandlers.HandleStdin(stdin)
		go ioHandlers.HandleStdout(stdout)
		go ioHandlers.HandleStderr(stderr)
	}

	go cmd.waitToExit()
	return
}

func PopenPTY(ioHandlers IOHandlers, cmdPath string, arg ...string) (cmd *Cmd, err error) {
	defer func() {
		if err == nil {
			return
		}
		if cmd == nil {
			return
		}
		cmd.closePTY()
		cmd = nil
	}()

	c := exec.Command(cmdPath, arg...)
	setEnvs(c, ioHandlers)

	m, s, e := pty.Open()
	if e != nil {
		err = e
		return
	}

	if err = makeRaw(s); err != nil {
		return
	}

	c.Stdout, c.Stderr, c.Stdin = s, s, s
	c.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true}
	if err = c.Start(); err != nil {
		return
	}

	cmd = &Cmd{
		cmd: c,
		Exit: make(chan struct{}),
		master: m,
		slave: s,
	}

	if ioHandlers != nil {
		go ioHandlers.HandleStdout(m)
		go ioHandlers.HandleStdin(m)
		go ioHandlers.HandleStderr(m)
	}

	go cmd.waitToExit()
	return
}

func setEnvs(c *exec.Cmd, ioHandlers IOHandlers) {
	if ioHandlers == nil {
		return
	}
	envs := ioHandlers.GetEnvs()
	if len(envs) == 0 {
		return
	}
	oldEnvs := c.Environ()
	for k, v := range envs {
		oldEnvs = append(oldEnvs, fmt.Sprintf("%s=%s", k, v))
	}
	c.Env = oldEnvs
}

func (cmd *Cmd) Wait() (exitCode int, err error) {
	<-cmd.Exit
	return cmd.cmd.ProcessState.ExitCode(), cmd.waitErr
}

func (cmd *Cmd) Close() {
	cmd.cmd.Process.Kill()
	cmd.closePTY()
}

func (cmd *Cmd) waitToExit() {
	cmd.waitErr = cmd.cmd.Wait()
	close(cmd.Exit)
	cmd.closePTY()
}

func (cmd *Cmd) closePTY() {
	if cmd.master != nil {
		cmd.master.Close()
		cmd.slave.Close()
	}
}

func makeRaw(s *os.File, setNonblock ...bool) error {
	fd := s.Fd()
	term.MakeRaw(int(fd))
	if len(setNonblock) > 0 && setNonblock[0] {
		return syscall.SetNonblock(int(fd), true)
	}
	return nil
}
