package expect

import (
	"regexp"
	"fmt"
	"io"
	"os"
	"time"
	"bytes"
	"syscall"
)

const (
	defaultTimeout = time.Second
)

var (
	NotFound = fmt.Errorf("not matched")
	TimedOut = fmt.Errorf("timed out")
)

type Case struct {
	Exp *regexp.Regexp
	SkipTill byte
	MatchedOnly bool
}

type Expect struct {
	out chan string
	cmd *Cmd
	timeout time.Duration

	in chan []byte
	buffer *bytes.Buffer
}

func Spawn(prog string, arg ...string) (e *Expect, err error) {
	return spawn(Popen, prog, arg...)
}

func SpawnPTY(prog string, arg ...string) (e *Expect, err error) {
	return spawn(PopenPTY, prog, arg...)
}

func spawn(popen fnPopen, prog string, arg ...string) (e *Expect, err error) {
	e = &Expect{
		out: make(chan string),
		timeout: defaultTimeout,
		in: make(chan []byte, 5),
		buffer: &bytes.Buffer{},
	}

	if e.cmd, err = popen(e, prog, arg...); err != nil {
		return
	}

	return
}

func (e *Expect) SetTimeout(d time.Duration) {
	e.timeout = d
}

func (e *Expect) Send(s string) {
	e.out <- s
}

func (e *Expect) HandleStdin(stdin io.WriteCloser) {
	for s := range e.out {
		io.WriteString(stdin, s)
	}
}

func (e *Expect) HandleStdout(stdout io.ReadCloser) {
	defer func() {
		close(e.in)
	}()

	for {
		select {
		case <-e.cmd.Exit:
			return
		default:
			buf := make([]byte, 1024)
			n, err := stdout.Read(buf)
			if err != nil {
				if pe, ok := err.(*os.PathError); ok {
					if errno, ok := pe.Err.(syscall.Errno); ok && errno == syscall.EAGAIN {
						time.Sleep(100 * time.Millisecond)
						continue
					}
				}
				return
			}

			if n <= 0 {
				continue
			}

			e.in <- buf[:n]
		}
	}
}

func (e *Expect) HandleStderr(stderr io.ReadCloser) {
}

func (e *Expect) Expect(expr string) ([]byte, error) {
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}
	_, m, err := e.ExpectCases(&Case{Exp: re, MatchedOnly: true})
	return m, err
}

func (e *Expect) ExpectRegexp(re *regexp.Regexp) ([]byte, error) {
	_, m, err := e.ExpectCases(&Case{Exp: re, MatchedOnly: true})
	return m, err
}

func (e *Expect) ExpectCases(cases ...*Case) (idx int, m []byte, err error) {
	if len(cases) == 0 {
		err = fmt.Errorf("cases expected")
		return
	}

	for {
		select {
		case <-e.cmd.Exit:
			err = io.EOF
			return
		case <-time.After(e.timeout):
		case data := <-e.in:
			if _, err = e.buffer.Write(data); err != nil {
				return
			}
		}

		if e.buffer.Len() == 0 {
			err = TimedOut
			return
		}
		buf := e.buffer.Bytes()
		afterSkip := false
		for i, c := range cases {
			loc := c.Exp.FindIndex(buf)
			if len(loc) == 0 {
				continue
			}
			if c.SkipTill > 0 {
				pos := bytes.IndexByte(buf, c.SkipTill)
				if pos >= 0 {
					e.buffer = bytes.NewBuffer(buf[pos+1:])
					afterSkip = true
					break
				}
			} else if c.MatchedOnly {
				e.buffer = bytes.NewBuffer(buf[loc[1]:])
				idx, m = i, buf[loc[0]:loc[1]]
				return
			}
			e.buffer.Reset()
			return i, buf, nil
		}
		if afterSkip {
			continue
		}
		err = NotFound
		return
	}
}

func (e *Expect) Wait() {
	e.cmd.Wait()
}

func (e *Expect) Close() {
	e.cmd.Close()
}

