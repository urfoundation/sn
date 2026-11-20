package miner

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/v2026/clientauth"
)

// A proxy slot's transports leave through its proxy, so the network operator
// counts it at no address of its own. It shows no head-breadth unit
// (WHITEPAPER.md §8.4), and a bound device earns no pool share. So binding a
// proxy slot gives up its pool share for nothing. fleetWarnProxySlot warns
// when the client being bound is one of this host's proxy slots. It looks in
// the provider state directory's proxy file and slot credentials, and in those
// of each operator directory below it. A client it doesn't find there, such as
// one bound from another host, gets no warning. Binding is never refused.
func fleetWarnProxySlot(w io.Writer, clientID [16]byte) {
	base, err := providerStateDir()
	if err != nil {
		return
	}
	directories := []string{base}
	if operators, err := filepath.Glob(filepath.Join(base, "operators", "*")); err == nil {
		directories = append(directories, operators...)
	}
	for _, directory := range directories {
		raw, err := os.ReadFile(filepath.Join(directory, "proxy"))
		if err != nil {
			continue
		}
		var proxyConfig ProxyConfig
		if err := json.Unmarshal(raw, &proxyConfig); err != nil {
			continue
		}
		for _, proxySettings := range proxySettingsOf(&proxyConfig) {
			jwt, err := os.ReadFile(filepath.Join(directory, providerClientJwtName(proxySettings)))
			if err != nil {
				continue
			}
			slotClientID, err := clientauth.ClientIdFromJwt(strings.TrimSpace(string(jwt)))
			if err != nil || [16]byte(slotClientID) != clientID {
				continue
			}
			fmt.Fprintf(w, "warning: client 0x%x is a proxy slot of this host (proxy %s, in %s). Its transports leave through its proxy, so it shows no head-breadth unit, and a bound device earns no pool share. Bind direct devices only.\n", clientID, proxySettings.Address, directory)
			return
		}
	}
}
