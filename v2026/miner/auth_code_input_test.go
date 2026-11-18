package miner

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadAuthCodePipedStdin(t *testing.T) {
	for _, input := range []string{"abc-_=123\r\n", "abc-_=123\n", "  abc-_=123  ", "abc-_=123\nignored\n"} {
		hidden := func() ([]byte, error) {
			t.Fatalf("piped stdin must not use the console hidden read")
			return nil, nil
		}
		authCode, err := readAuthCode(strings.NewReader(input), io.Discard, false, hidden)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if authCode != "abc-_=123" {
			t.Fatalf("input %q: auth code = %q", input, authCode)
		}
	}
}

func TestReadAuthCodeTerminalTrimsAndSaysInputIsHidden(t *testing.T) {
	var out bytes.Buffer
	authCode, err := readAuthCode(strings.NewReader(""), &out, true, func() ([]byte, error) {
		return []byte(" abc-_=123\r"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if authCode != "abc-_=123" {
		t.Fatalf("auth code = %q", authCode)
	}
	if !strings.Contains(out.String(), "hidden") {
		t.Fatalf("prompt does not say input is hidden: %q", out.String())
	}
	if strings.Contains(out.String(), "abc") {
		t.Fatalf("auth code was echoed: %q", out.String())
	}
}

func TestReadAuthCodeRejectsEmpty(t *testing.T) {
	if _, err := readAuthCode(strings.NewReader("\r\n"), io.Discard, false, nil); !errors.Is(err, errEmptyAuthCode) {
		t.Fatalf("empty piped code: err = %v", err)
	}
	if _, err := readAuthCode(strings.NewReader(""), io.Discard, false, nil); err == nil {
		t.Fatalf("closed stdin was accepted")
	}
	if _, err := readAuthCode(nil, io.Discard, true, func() ([]byte, error) { return []byte("  "), nil }); !errors.Is(err, errEmptyAuthCode) {
		t.Fatalf("empty terminal code: err = %v", err)
	}
}
