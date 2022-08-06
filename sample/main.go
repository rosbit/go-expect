package main

import (
	"github.com/rosbit/go-expect"
	"os"
	"io"
	"fmt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <exe> <arg>...\n", os.Args[0])
		return
	}

	cmd, err := expect.Popen(&cmdtest{}, os.Args[1], os.Args[2:]...)
	// cmd, err := expect.PopenPTY(&cmdtest{}, os.Args[1], os.Args[2:]...)
	if err != nil {
		fmt.Printf("failed to Popen: %v\n", err)
		return
	}

	cmd.Wait()
}

func iocopy(to io.Writer, from io.Reader) {
	io.Copy(to, from)
}

type cmdtest struct{}

func (c *cmdtest) HandleStdin(stdin io.WriteCloser) {
	iocopy(stdin, os.Stdin)
}

func (c *cmdtest) HandleStdout(stdin io.ReadCloser) {
	iocopy(os.Stdout, stdin)
}

func (c *cmdtest) HandleStderr(stderr io.ReadCloser) {
	iocopy(os.Stderr, stderr)
}
