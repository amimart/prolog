package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/ichiban/prolog"
	"github.com/ichiban/prolog/engine"
)

const (
	prompt          = "?- "
	contPrompt      = "|- "
	userInputPrompt = "|: "
)

var version = func() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	return info.Main.Version
}()

func main() {
	var verbose bool
	flag.BoolVar(&verbose, "v", false, `verbose`)
	flag.Parse()

	fmt.Printf(`Top level for ichiban/prolog %s
This is for testing purposes only!
See https://github.com/ichiban/prolog for more details.
Type Ctrl-C or 'halt.' to exit.
`, version)

	halt := engine.Halt
	if terminal.IsTerminal(0) {
		oldState, err := terminal.MakeRaw(0)
		if err != nil {
			log.Panicf("failed to enter raw mode: %v", err)
		}
		restore := func() {
			_ = terminal.Restore(0, oldState)
		}
		defer restore()

		halt = func(vm *engine.VM, n engine.Term, k func(*engine.Env) *engine.Promise, env *engine.Env) *engine.Promise {
			restore()
			return engine.Halt(vm, n, k, env)
		}
	}

	t := terminal.NewTerminal(os.Stdin, prompt)
	defer fmt.Printf("\r\n")

	log.SetOutput(t)

	i := New(&userInput{t: t}, t)
	i.Register1("halt", halt)
	if verbose {
		i.OnCall = func(pi engine.ProcedureIndicator, args []engine.Term, env *engine.Env) {
			log.Printf("CALL %s", goal(i, pi, args, env))
		}
		i.OnExit = func(pi engine.ProcedureIndicator, args []engine.Term, env *engine.Env) {
			log.Printf("EXIT %s", goal(i, pi, args, env))
		}
		i.OnFail = func(pi engine.ProcedureIndicator, args []engine.Term, env *engine.Env) {
			log.Printf("FAIL %s", goal(i, pi, args, env))
		}
		i.OnRedo = func(pi engine.ProcedureIndicator, args []engine.Term, env *engine.Env) {
			log.Printf("REDO %s", goal(i, pi, args, env))
		}
	}
	i.OnUnknown = func(pi engine.ProcedureIndicator, args []engine.Term, env *engine.Env) {
		log.Printf("UNKNOWN %s", goal(i, pi, args, env))
	}

	// Consult arguments.
	if err := i.QuerySolution(`consult(?).`, flag.Args()).Err(); err != nil {
		log.Panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var buf strings.Builder
	keys := bufio.NewReader(os.Stdin)
	for {
		switch err := handleLine(ctx, &buf, i, t, keys); err {
		case nil:
			break
		case io.EOF:
			return
		default:
			log.Panic(err)
		}
	}
}

func goal(i *prolog.Interpreter, pi engine.ProcedureIndicator, args []engine.Term, env *engine.Env) string {
	goal, _ := pi.Apply(args...)
	var buf bytes.Buffer
	_ = engine.WriteTerm(&buf, goal, &engine.WriteOptions{Quoted: true}, env)
	return buf.String()
}

func handleLine(ctx context.Context, buf *strings.Builder, p *prolog.Interpreter, t *terminal.Terminal, keys *bufio.Reader) (err error) {
	line, err := t.ReadLine()
	if err != nil {
		return err
	}
	_, _ = buf.WriteString(line)
	_, _ = buf.WriteString("\n")

	sols, err := p.QueryContext(ctx, buf.String())
	switch err {
	case nil:
		buf.Reset()
		t.SetPrompt(prompt)
	case io.EOF:
		// Returns without resetting buf.
		t.SetPrompt(contPrompt)
		return nil
	default:
		log.Printf("failed to query: %v", err)
		buf.Reset()
		t.SetPrompt(prompt)
		return nil
	}
	defer func() {
		_ = sols.Close()
	}()

	var exists bool
	for sols.Next() {
		exists = true

		m := map[string]engine.Term{}
		_ = sols.Scan(m)

		var buf bytes.Buffer
		vars := sols.Vars()
		if len(vars) == 0 {
			_, _ = fmt.Fprintf(&buf, "%t", true)
		} else {
			ls := make([]string, len(vars))
			for i, v := range vars {
				var sb strings.Builder
				_, _ = fmt.Fprintf(&sb, "%s = ", v)
				if err := p.Write(&sb, m[v], &engine.WriteOptions{Quoted: true}, nil); err != nil {
					return err
				}
				ls[i] = sb.String()
			}
			_, _ = fmt.Fprint(&buf, strings.Join(ls, ",\n"))
		}
		if _, err := t.Write(buf.Bytes()); err != nil {
			return err
		}

		r, _, err := keys.ReadRune()
		if err != nil {
			return err
		}
		if r != ';' {
			r = '.'
		}
		if _, err := fmt.Fprintf(t, "%s\n", string(r)); err != nil {
			return err
		}
		if r == '.' {
			break
		}
	}

	if err := sols.Err(); err != nil {
		log.Print(err)
		return nil
	}

	if !exists {
		if _, err := fmt.Fprintf(t, "%t.\n", false); err != nil {
			return err
		}
	}

	return nil
}

type userInput struct {
	t   *terminal.Terminal
	buf bytes.Buffer
}

func (u *userInput) Read(p []byte) (n int, err error) {
	if u.buf.Len() == 0 {
		u.t.SetPrompt(userInputPrompt)
		defer u.t.SetPrompt(prompt)
		line, err := u.t.ReadLine()
		if err != nil {
			return 0, err
		}
		u.buf.WriteString(line + "\n")
	}

	return u.buf.Read(p)
}

func (u *userInput) Write(b []byte) (n int, err error) {
	return 0, nil
}
