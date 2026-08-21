module github.com/urfoundation/sn/v2026

go 1.26.5

require (
	github.com/centrifuge/go-substrate-rpc-client/v4 v4.2.2-0.20240919131012-e3b938563803
	github.com/consensys/gnark-crypto v0.20.1
	github.com/docopt/docopt-go v0.0.0-20180111231733-ee0de3bc6815
	github.com/ethereum/go-ethereum v1.16.7
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/vedhavyas/go-subkey/v2 v2.0.0
	golang.org/x/crypto v0.54.0
	golang.org/x/net v0.57.0
	golang.org/x/term v0.45.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/ChainSafe/go-schnorrkel v1.0.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/ProjectZKM/Ziren/crates/go-runtime/zkvm_runtime v0.0.0-20251001021608-1fe7b43fc4d6 // indirect
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.4 // indirect
	github.com/cosmos/go-bip39 v1.0.0 // indirect
	github.com/crate-crypto/go-eth-kzg v1.4.0 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/deckarep/golang-set v1.8.0 // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/decred/base58 v1.0.4 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.5 // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/gtank/merlin v0.1.1 // indirect
	github.com/gtank/ristretto255 v0.1.2 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/mimoo/StrobeGo v0.0.0-20220103164710-9a04d6ca976b // indirect
	github.com/pierrec/xxHash v0.1.5 // indirect
	github.com/pion/datachannel v1.6.2 // indirect
	github.com/pion/dtls/v3 v3.1.5 // indirect
	github.com/pion/ice/v4 v4.4.1 // indirect
	github.com/pion/interceptor v0.1.47 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.1.0 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.17 // indirect
	github.com/pion/rtp v1.10.5 // indirect
	github.com/pion/sctp v1.11.1 // indirect
	github.com/pion/sdp/v3 v3.0.19 // indirect
	github.com/pion/srtp/v3 v3.0.13 // indirect
	github.com/pion/stun/v3 v3.1.6 // indirect
	github.com/pion/transport/v4 v4.1.0 // indirect
	github.com/pion/turn/v5 v5.0.12 // indirect
	github.com/pion/webrtc/v4 v4.2.18 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/quic-go/quic-go v0.61.0 // indirect
	github.com/rs/cors v1.8.2 // indirect
	github.com/shirou/gopsutil v3.21.4-0.20210419000835-c7a38de76ee5+incompatible // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/supranational/blst v0.3.16-0.20250831170142-f48500c1fdbe // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/urnetwork/connect/v2026 v2026.8.21-1025223880
	github.com/urnetwork/glog/v2026 v2026.8.21-1025223880 // indirect
	github.com/urnetwork/goidenticons/v2026 v2026.8.21-1025223880 // indirect
	github.com/urnetwork/sdk/v2026 v2026.8.21-1025223880
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/exp v0.0.0-20260727155853-b88d891fe743 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/natefinch/npipe.v2 v2.0.0-20160621034901-c1b8fa8bdcce // indirect
	gvisor.dev/gvisor v0.0.0-20260805230438-8eba670122c5 // indirect
	src.agwa.name/tlshacks v0.0.4 // indirect
)

// The substrate RPC client still imports the archived npipe module, whose
// generated Windows syscalls only support 386 and amd64. Keep its narrow API
// behind an arm64-capable go-winio adapter until the upstream import is fixed.
replace gopkg.in/natefinch/npipe.v2 => ./third_party/npipe
