package miner

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// The auth code prompt reads without echo on a terminal. Nothing appears while
// typing or pasting, so the prompt says so; otherwise users conclude the prompt
// accepts no input.
const authCodePrompt = "Enter auth code (input is hidden; paste it and press Enter): "

var errEmptyAuthCode = errors.New("no auth code entered")

// readAuthCode prompts for an auth code. On a terminal it reads without echo
// via readHidden. When stdin is not a terminal (piped or redirected, e.g.
// `echo <code> | urnetwork auth`) there is no console mode to change, so the
// first line of in is read instead. Surrounding whitespace, including a CRLF
// line ending, is not part of the code.
func readAuthCode(in io.Reader, out io.Writer, isTerminal bool, readHidden func() ([]byte, error)) (string, error) {
	fmt.Fprint(out, authCodePrompt)
	var raw string
	if isTerminal {
		authCodeBytes, err := readHidden()
		fmt.Fprint(out, "\n")
		if err != nil {
			return "", err
		}
		raw = string(authCodeBytes)
	} else {
		line, err := bufio.NewReader(in).ReadString('\n')
		fmt.Fprint(out, "\n")
		if err != nil && !(errors.Is(err, io.EOF) && line != "") {
			return "", err
		}
		raw = line
	}
	authCode := strings.TrimSpace(raw)
	if authCode == "" {
		return "", errEmptyAuthCode
	}
	return authCode, nil
}
