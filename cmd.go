package expect

import (
	"github.com/creack/pty"
	"golang.org/x/term"
	"os/exec"
	"io"
	"os"
	"syscall"
)

type IOHandlers interface {
	HandleStdin(io.WriteCloser)
	HandleStdout(io.ReadCloser)
	HandleStderr(io.ReadCloser)
}

type Cmd struct {
	cmd *exec.Cmd
	Exit chan struct{}
	master, slave *os.File
}

type fnPopen func(ioHandlers IOHandlers, cmdPath string, arg ...string) (cmd *Cmd, err error)

func Popen(ioHandlers IOHandlers, cmdPath string, arg ...string) (cmd *Cmd, err error) {
	c := exec.Command(cmdPath, arg...)

	stdin, _ := c.StdinPipe()
	stdout, _ := c.StdoutPipe()
	if s, ok := stdout.(*os.File); ok {
		makeRaw(s, true)
	}
	stderr, _ := c.StderrPipe()

	if err = c.Start(); err != nil {
		return
	}

	if ioHandlers != nil {
		go ioHandlers.HandleStdout(stdout)
		go ioHandlers.HandleStdin(stdin)
		go ioHandlers.HandleStderr(stderr)
	}

	cmd = &Cmd{
		cmd: c,
		Exit: make(chan struct{}),
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

	if ioHandlers != nil {
		go ioHandlers.HandleStdout(m)
		go ioHandlers.HandleStdin(m)
		go ioHandlers.HandleStderr(m)
	}

	cmd = &Cmd{
		cmd: c,
		Exit: make(chan struct{}),
		master: m,
		slave: s,
	}

	go cmd.waitToExit()
	return
}

func (cmd *Cmd) Wait() {
	<-cmd.Exit
}

func (cmd *Cmd) Close() {
	cmd.cmd.Process.Kill()
	cmd.closePTY()
}

func (cmd *Cmd) waitToExit() {
	cmd.cmd.Wait()
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
