package miner

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// errPromptInterrupted is what a prompt returns when Ctrl-C ends it.
var errPromptInterrupted = fmt.Errorf("interrupted")

// readHiddenInterruptible reads a line without echo, like term.ReadPassword,
// but Ctrl-C (or SIGTERM) ends the read: the terminal gets its echo back and
// the caller gets errPromptInterrupted. Other signal handlers in the process
// (the miner's own) keep the signal from ending it otherwise.
func readHiddenInterruptible(fd int) ([]byte, error) {
	state, stateErr := term.GetState(fd)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	type result struct {
		line []byte
		err  error
	}
	read := make(chan result, 1)
	go func() {
		line, err := term.ReadPassword(fd)
		read <- result{line, err}
	}()
	select {
	case r := <-read:
		return r.line, r.err
	case <-signals:
		if stateErr == nil {
			_ = term.Restore(fd, state)
		}
		return nil, errPromptInterrupted
	}
}

// cliAsk asks a yes/no question on the terminal; Enter takes the default.
// Ctrl-C cancels the whole command, as it does at the auth code prompt.
func cliAsk(in io.Reader, out io.Writer, question string, yesByDefault bool) bool {
	choices := "[y/N]"
	if yesByDefault {
		choices = "[Y/n]"
	}
	fmt.Fprintf(out, "%s %s ", question, choices)
	answer, ok := cliReadLine(in)
	if !ok {
		fmt.Fprintln(out)
		panic(errPromptInterrupted)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "":
		return yesByDefault
	case "y", "yes":
		return true
	}
	return false
}

// cliReadLine reads one line, or reports false on Ctrl-C or end of input.
func cliReadLine(in io.Reader) (string, bool) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	type result struct {
		line string
		err  error
	}
	read := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(in).ReadString('\n')
		read <- result{line, err}
	}()
	select {
	case r := <-read:
		if r.err != nil && r.line == "" {
			return "", false
		}
		return strings.TrimRight(r.line, "\r\n"), true
	case <-signals:
		return "", false
	}
}
