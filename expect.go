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

type Action uint8
const (
	Continue Action = iota
	Break
)
type FnMatched func(m []byte) Action

type Case struct {
	Exp *regexp.Regexp
	SkipTill byte
	MatchedOnly bool
	ExpMatched FnMatched
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

func (e *Expect) Expect(expr string, expMathed ...FnMatched) ([]byte, error) {
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}
	_, m, err := e.ExpectCases(&Case{Exp: re, MatchedOnly: true, ExpMatched: func()FnMatched{
		if len(expMathed) > 0 {
			return expMathed[0]
		}
		return nil
	}()})
	return m, err
}

func (e *Expect) ExpectRegexp(re *regexp.Regexp, expMathed ...FnMatched) ([]byte, error) {
	_, m, err := e.ExpectCases(&Case{Exp: re, MatchedOnly: true, ExpMatched: func()FnMatched{
		if len(expMathed) > 0 {
			return expMathed[0]
		}
		return nil
	}()})
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
Again:
		for i, c := range cases {
			loc := c.Exp.FindIndex(buf)
			if len(loc) == 0 {
				continue
			}
			if c.SkipTill > 0 {
				pos := bytes.IndexByte(buf, c.SkipTill)
				if pos >= 0 {
					if pos == len(buf) - 1 {
						e.buffer.Reset()
						afterSkip = true
						break
					}
					buf = buf[pos+1:]
					goto Again
				}
			} else if c.MatchedOnly {
				idx, m = i, buf[loc[0]:loc[1]]
				if c.ExpMatched == nil || c.ExpMatched(m) != Continue {
					e.buffer = bytes.NewBuffer(buf[loc[1]:])
					return
				}
				if loc[1] == len(buf) {
					e.buffer.Reset()
					afterSkip = true
					break
				}
				buf = buf[loc[1]:]
				goto Again
			}

			if c.ExpMatched == nil {
				idx, m = i, buf
				e.buffer.Reset()
				return
			}

			idx, m = i, buf[loc[0]:loc[1]]
			if c.ExpMatched(m) != Continue {
				e.buffer = bytes.NewBuffer(buf[loc[1]:])
				return
			}
			if loc[1] == len(buf) {
				e.buffer.Reset()
				afterSkip = true
				break
			}
			buf = buf[loc[1]:]
			goto Again
		}
		if afterSkip {
			continue
		}
		err = NotFound
		return
	}
}

func (e *Expect) Wait() (exitCode int, err error) {
	return e.cmd.Wait()
}

func (e *Expect) Close() {
	e.cmd.Close()
}

