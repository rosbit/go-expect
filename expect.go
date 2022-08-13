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

	removeColorRE = regexp.MustCompile("\x1b\\[[0-9;]*[mK]")
)

type Action uint8
const (
	Continue Action = iota
	Break
)
type FnMatched func(m []byte) Action
type FnNotMatched func(m []byte) (skipN int)

type Case struct {
	Exp *regexp.Regexp
	SkipTill byte
	MatchedOnly bool
	ExpMatched FnMatched
}

type Expect struct {
	out chan string
	cmd *Cmd
	envs map[string]string
	timeout time.Duration
	removeColor bool

	in chan []byte
	buffer *bytes.Buffer
	notMatched FnNotMatched
}

func Spawn(prog string, arg ...string) (e *Expect, err error) {
	return spawn(nil, Popen, prog, arg...)
}

func SpawnWithEnvs(envs map[string]string, prog string, arg ...string) (e *Expect, err error) {
	return spawn(envs, Popen, prog, arg...)
}

func SpawnPTY(prog string, arg ...string) (e *Expect, err error) {
	return spawn(nil, PopenPTY, prog, arg...)
}

func SpawnPTYWithEnvs(envs map[string]string, prog string, arg ...string) (e *Expect, err error) {
	return spawn(envs, PopenPTY, prog, arg...)
}

func spawn(envs map[string]string, popen fnPopen, prog string, arg ...string) (e *Expect, err error) {
	e = &Expect{
		out: make(chan string),
		timeout: defaultTimeout,
		in: make(chan []byte, 5),
		buffer: &bytes.Buffer{},
		envs: envs,
	}

	if e.cmd, err = popen(e, prog, arg...); err != nil {
		return
	}

	return
}

func (e *Expect) SetTimeout(d time.Duration) {
	e.timeout = d
}

func (e *Expect) RemoveColor() {
	e.removeColor = true
}

func (e *Expect) SetNotMatchedHandler(notMatched FnNotMatched) {
	e.notMatched = notMatched
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

			if !e.removeColor {
				e.in <- buf[:n]
			} else {
				e.in <- removeColorRE.ReplaceAll(buf[:n], []byte(""))
			}
		}
	}
}

func (e *Expect) HandleStderr(stderr io.ReadCloser) {
}

func (e *Expect) GetEnvs() map[string]string {
	return e.envs
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
		// fmt.Printf("buf: >>>%s<<<, len: %d, %x\n", buf, len(buf), buf)
		for i, c := range cases {
			loc := c.Exp.FindIndex(buf)
			// fmt.Printf(" i: %d, loc: %v\n", i, loc)
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
				if c.ExpMatched == nil {
					e.buffer = bytes.NewBuffer(buf[loc[1]:])
					return
				}
				goto CallExpMatched
			}

			if c.ExpMatched == nil {
				idx, m = i, buf
				e.buffer.Reset()
				return
			}

			idx, m = i, buf[loc[0]:loc[1]]
CallExpMatched:
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

		if e.notMatched == nil {
			err = NotFound
			return
		}
		skipN := e.notMatched(m)
		if skipN <= 0 {
			continue
		}
		if skipN >= len(buf) {
			e.buffer.Reset()
		} else {
			e.buffer = bytes.NewBuffer(buf[skipN:])
		}
	}
}

func (e *Expect) Wait() (exitCode int, err error) {
	return e.cmd.Wait()
}

func (e *Expect) Close() {
	e.cmd.Close()
}

