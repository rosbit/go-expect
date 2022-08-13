# simple expect in golang

## Usage

1. Call expect.Popen/PopenPTY to execute command

    See [sample](sample/main.go).

2. Call expect.Spawn/SpawnPTY to implement TCL-like `expect`

    - [go-trealla](https://github.com/rosbit/go-trealla), interacts with trealla, a Prolog interpreter.
    - [go-qjs](https://github.com/rosbit/go-qjs), interacts with QuickJS, a JavaScript engine.
    - [go-deno](https://github.com/rosbit/go-deno), interacts with Deno, a JavaScript/TypeScript runtime engine.
