// Operators need a failed steering attempt's causes while the validator still
// runs, not only from the graph a panic or shutdown prints. This summary reads
// an observed release error graph and prints only text that graph already
// renders: observed wrappers add none, native and network read originals stay
// behind their markers, and URL, network and file operations keep their
// operation. Every leaf is redacted for credentials and bounded first.
package validator

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The walk is finite even over a joined graph of many observations.
const (
	releaseErrorCauseMaximumNodes     = 2048
	releaseErrorCauseMaximumDepth     = 96
	releaseErrorCauseMaximumDistinct  = 256
	releaseErrorCauseMaximumLeafBytes = 240
	releaseErrorCauseMaximumLineBytes = 1024
)

// One distinct redacted leaf text and how often the graph reached it.
type releaseErrorCause struct {
	text  string
	count int
}

type releaseErrorCauseWalk struct {
	remaining int
	truncated bool
	order     []string
	counts    map[string]int
}

// Distinct leaves, most frequent first and otherwise in graph order. The
// graph is observed first, so every node visited is a frozen release value.
func releaseErrorCauses(err error) ([]releaseErrorCause, bool) {
	walk := &releaseErrorCauseWalk{remaining: releaseErrorCauseMaximumNodes, counts: make(map[string]int)}
	walk.visit(observeReleaseError(err), 0)
	causes := make([]releaseErrorCause, 0, len(walk.order))
	for _, text := range walk.order {
		causes = append(causes, releaseErrorCause{text: text, count: walk.counts[text]})
	}
	sort.SliceStable(causes, func(i, j int) bool { return causes[i].count > causes[j].count })
	return causes, walk.truncated
}

// Observation already replaced every foreign wrapper with a release wrapper,
// so Unwrap is only ever called on this package's own frozen values.
func (self *releaseErrorCauseWalk) visit(err error, depth int) {
	if err == nil {
		return
	}
	if depth > releaseErrorCauseMaximumDepth || self.remaining <= 0 {
		self.truncated = true
		return
	}
	self.remaining--
	if value := reflect.ValueOf(err); value.Kind() == reflect.Pointer && value.IsNil() {
		self.add("nil error")
		return
	}
	switch cause := err.(type) {
	case *releaseObservedOriginal:
		self.visit(cause.value, depth+1)
	case *releaseObservedCause:
		self.visit(cause.cause, depth+1)
	case *releaseObservedCauses:
		for _, child := range cause.causes {
			self.visit(child, depth+1)
		}
	case *releaseObservedHard:
		self.visit(cause.cause, depth+1)
	case *releaseObservedNativeRead:
		self.add(fmt.Sprintf("native read failed (retryable=%t)", cause.retryable))
	case *releaseObservedNetworkRead:
		self.add(fmt.Sprintf("network read failed (retryable=%t)", cause.retryable))
	case *url.Error:
		self.add(cause.Op + " " + strconv.Quote(cause.URL) + ": " + self.summary(cause.Err, depth+1))
	case *net.OpError:
		self.add(releaseNetOpErrorText(cause) + ": " + self.summary(cause.Err, depth+1))
	case *os.PathError, *os.LinkError, *os.SyscallError, *net.DNSError, *TrailFatalError:
		// Observation keeps these values whole; shutdown prints them the same way.
		self.add(err.Error())
	default:
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			for _, child := range joined.Unwrap() {
				self.visit(child, depth+1)
			}
			return
		}
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			if child := wrapped.Unwrap(); child != nil {
				self.visit(child, depth+1)
				return
			}
		}
		self.add(err.Error())
	}
}

// An operation's own cause collapses to its distinct leaves in graph order.
func (self *releaseErrorCauseWalk) summary(err error, depth int) string {
	if err == nil {
		return "<nil>"
	}
	inner := &releaseErrorCauseWalk{remaining: self.remaining, counts: make(map[string]int)}
	inner.visit(err, depth)
	self.remaining = inner.remaining
	self.truncated = self.truncated || inner.truncated
	if len(inner.order) == 0 {
		return "unknown"
	}
	return strings.Join(inner.order, "; ")
}

func (self *releaseErrorCauseWalk) add(text string) {
	text = boundReleaseErrorText(redactReleaseErrorText(text), releaseErrorCauseMaximumLeafBytes)
	if _, seen := self.counts[text]; !seen {
		if len(self.order) >= releaseErrorCauseMaximumDistinct {
			self.truncated = true
			return
		}
		self.order = append(self.order, text)
	}
	self.counts[text]++
}

// net.OpError.Error without its cause, which the walk summarizes separately.
func releaseNetOpErrorText(cause *net.OpError) string {
	text := cause.Op
	if cause.Net != "" {
		text += " " + cause.Net
	}
	if cause.Source != nil {
		text += " " + cause.Source.String()
	}
	if cause.Addr != nil {
		if cause.Source != nil {
			text += "->"
		} else {
			text += " "
		}
		text += cause.Addr.String()
	}
	return text
}

// Credentials can reach error text only through HTTP: URL userinfo, token
// parameters, Authorization or Bearer values, and JWTs or token fields in
// response bodies. Seeds and keys are read from files whose errors name only
// the path, and Go's HTTP errors never include request headers.
var (
	releaseErrorJWT           = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)
	releaseErrorAuthorization = regexp.MustCompile(`(?i)\b((?:proxy-)?authorization\s*[:=]\s*)(?:[A-Za-z]+\s+)?[^\s,;"']+`)
	releaseErrorBearer        = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]{8,}`)
	releaseErrorUserinfo      = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/?#@\s"']*@`)
	releaseErrorSecretParam   = regexp.MustCompile(`(?i)((?:^|[\s?&;,{(])(?:access_token|refresh_token|id_token|token|jwt|by_jwt|auth|api_?key|key|secret|client_secret|password|passwd|pwd|signature|sig|credentials?|session|session_?id)=)[^&#\s"',;)}]*`)
	releaseErrorSecretField   = regexp.MustCompile(`(?i)("(?:access_token|refresh_token|id_token|token|jwt|by_jwt|auth|authorization|api_?key|secret|client_secret|password|passwd|pwd|signature|credentials?|session_?id)"\s*:\s*")[^"]*`)
)

func redactReleaseErrorText(text string) string {
	text = releaseErrorJWT.ReplaceAllString(text, "<redacted-jwt>")
	text = releaseErrorAuthorization.ReplaceAllString(text, "${1}<redacted>")
	text = releaseErrorBearer.ReplaceAllString(text, "${1} <redacted>")
	text = releaseErrorUserinfo.ReplaceAllString(text, "${1}<redacted>@")
	text = releaseErrorSecretParam.ReplaceAllString(text, "${1}<redacted>")
	text = releaseErrorSecretField.ReplaceAllString(text, "${1}<redacted>")
	return text
}

// One line of valid UTF-8 without control characters, cut on a rune boundary.
func boundReleaseErrorText(text string, limit int) string {
	text = strings.ToValidUTF8(text, "?")
	text = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, text)
	if len(text) <= limit {
		return text
	}
	cut := limit - len("…")
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// One record per failed steering attempt: the scalar event's code and attempt
// and the distinct leaf causes, never more than 1 KiB. A formatter panic from
// any leaf only replaces the causes with a fixed marker.
func formatReleaseErrorCauses(code string, attempt uint64, err error) (raw []byte) {
	defer func() {
		if recover() != nil {
			raw = []byte(fmt.Sprintf("validator steering causes: code=%s attempt=%d causes unavailable: a leaf could not be formatted\n", code, attempt))
		}
	}()
	causes, truncated := releaseErrorCauses(err)
	total := 0
	for _, cause := range causes {
		total += cause.count
	}
	var line strings.Builder
	fmt.Fprintf(&line, "validator steering causes: code=%s attempt=%d leaves=%d distinct=%d", code, attempt, total, len(causes))
	if truncated {
		line.WriteString(" truncated=true")
	}
	for index, cause := range causes {
		separator := " | "
		if index == 0 {
			separator = ": "
		}
		item := fmt.Sprintf("%s%dx %s", separator, cause.count, cause.text)
		reserve := 0
		if index+1 < len(causes) {
			reserve = len(fmt.Sprintf(" | +%d more", len(causes)-index-1))
		}
		if line.Len()+len(item)+reserve+len("\n") > releaseErrorCauseMaximumLineBytes {
			fmt.Fprintf(&line, " | +%d more", len(causes)-index)
			break
		}
		line.WriteString(item)
	}
	line.WriteString("\n")
	return []byte(line.String())
}
