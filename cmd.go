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

type PTY struct {
	Master *os.File
	Slave  *os.File
}

func Popen(ioHandlers IOHandlers, cmdPath string, arg ...string) (cmd *exec.Cmd, p *PTY, err error) {
	defer func() {
		if err == nil {
			return
		}
		if p == nil {
			return
		}
		p.Master.Close()
		p.Slave.Close()
		p = nil
	}()

	cmd = exec.Command(cmdPath, arg...)

	m, s, e := pty.Open()
	if e != nil {
		err = e
		return
	}
	p = &PTY{
		Master: m,
		Slave: s,
	}

	if err = makeRaw(m, s); err != nil {
		return
	}

	cmd.Stdout, cmd.Stderr, cmd.Stdin = s, s, s
	cmd.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true}
	if err = cmd.Start(); err != nil {
		return
	}

	if ioHandlers != nil {
		go ioHandlers.HandleStdout(m)
		go ioHandlers.HandleStdin(m)
		go ioHandlers.HandleStderr(m)
	}

	return
}

func makeRaw(m, s *os.File) error {
	fd := s.Fd()
	term.MakeRaw(int(fd))
	return nil
}
