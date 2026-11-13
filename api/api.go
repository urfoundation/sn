// Package api holds the OpenAPI documents of the routes a network operator's
// server serves for SN25. operator.yml describes the public capture the flag
// routes under /sn/ctf and the ledger account route; the server serves it at
// GET /sn/openapi.yml from OperatorYml, so the document it serves is always
// the one its release was built with. competition.yml describes /competition.
package api

import _ "embed"

// OperatorYml is operator.yml, byte for byte.
//
//go:embed operator.yml
var OperatorYml []byte
