package bridge

import "github.com/urfoundation/sn/demand/solana"

// The accounts of an OFT send from Solana to Bittensor, in order, as every send
// on mainnet passes them (read from SxQAqB3qnPsDv2mw… and its neighbours on
// 2026-11-02, testdata/send_mainnet.json). Only the sender and its token account
// vary. LayerZero can change its verifier or executor settings; a list gone stale
// in a way the program checks fails the send's simulation, which runs on the
// unsigned transaction, so such a send is refused with nothing signed.
var (
	OFTProgram = solana.MustPublicKey("tao3RyGP8XiiWQKmBzkiULmPoMewWjq65b46H4rTAQQ")
	OFTStore   = solana.MustPublicKey("8vJKzzabD9t15SwVa8aQUEJH37Xk5nS9eaQ4WgojZdDg")
	PeerConfig = solana.MustPublicKey("JT6nhHYre8hUymLBTW6zui425C5zbK4aZhcnvnE72x8")
	TaoMint    = solana.MustPublicKey("taoC6xyv2v8tDLcev4uaGUgV4vdQsWJrGft2kcBRrBY")
	// LookupTable is LayerZero's public address lookup table, without which the
	// send does not fit in a packet.
	LookupTable = solana.MustPublicKey("AokBxha6VMLLgf97B5VYHEtqztamWmYERBmmFvjuTzJB")
)

type pinnedAccount struct {
	key      solana.PublicKey
	writable bool
}

// sendAccounts holds the fixed positions of the list; position 0 is the sender,
// 3 its token account and 22 the payer of the verifiers and executor (the sender).
var sendAccounts = map[int]pinnedAccount{
	1:  {solana.MustPublicKey("JT6nhHYre8hUymLBTW6zui425C5zbK4aZhcnvnE72x8"), true},   // peer config
	2:  {solana.MustPublicKey("8vJKzzabD9t15SwVa8aQUEJH37Xk5nS9eaQ4WgojZdDg"), true},  // OFT store
	4:  {solana.MustPublicKey("FeiTZPe7uJYJLux1CahrQnU94SjSXQ6zsdgobLm658LN"), true},  // token escrow
	5:  {solana.MustPublicKey("taoC6xyv2v8tDLcev4uaGUgV4vdQsWJrGft2kcBRrBY"), true},   // TAO mint
	6:  {solana.MustPublicKey("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"), false},  // token program
	7:  {solana.MustPublicKey("CZr3YE5mNB5nXrA6yzK3DdaeGSxbMUt8CQeq4MG9usxv"), false}, // OFT event authority
	8:  {solana.MustPublicKey("tao3RyGP8XiiWQKmBzkiULmPoMewWjq65b46H4rTAQQ"), false},  // OFT program
	9:  {solana.MustPublicKey("76y77prsiCMvXMjuoZ5VRrhG5qYBrUMYTE5WgHqgjEn6"), false}, // endpoint program
	10: {solana.MustPublicKey("8vJKzzabD9t15SwVa8aQUEJH37Xk5nS9eaQ4WgojZdDg"), true},  // OApp (the OFT store)
	11: {solana.MustPublicKey("7a4WjyR8VZ7yZz5XJAKm39BUGn5iT9CKcv2pmG9tdXVH"), false}, // send library (ULN)
	12: {solana.MustPublicKey("FoDnQkwZX1zi6WAP1evqKYTbeu8N2g7mEjbeBv7hpXXa"), false}, // send library config
	13: {solana.MustPublicKey("FN7XushjTMppsw4n9V5KM3ou7QpyxYWbLm7Tgtr74p98"), false}, // default send library config
	14: {solana.MustPublicKey("526PeNZfw8kSnDU4nmzJFVJzJWNhwmZykEyJr5XWz5Fv"), false}, // send library info
	15: {solana.MustPublicKey("2uk9pQh3tB5ErV7LGQJcbWjb4KeJ2UJki5qJZ8QG56G3"), false}, // endpoint settings
	16: {solana.MustPublicKey("FnJseVjPYDr9JYgqK4tySYYzSNZUCvxoKQvwB7j1vEe2"), true},  // nonce
	17: {solana.MustPublicKey("F8E8QGhKmHEx2esh5LpVizzcP4cHYhzXdXTwg9w3YYY2"), false}, // endpoint event authority
	18: {solana.MustPublicKey("76y77prsiCMvXMjuoZ5VRrhG5qYBrUMYTE5WgHqgjEn6"), false}, // endpoint program
	19: {solana.MustPublicKey("2XgGZG4oP29U3w5h4nTk1V2LFHL23zKDPJjs3psGzLKQ"), false}, // ULN settings
	20: {solana.MustPublicKey("3LecQ6ugec9qqdwJgtJspuzmRbbEshXF8zvewDuEpb2i"), false}, // ULN send config
	21: {solana.MustPublicKey("FzxKkGtzUfXTEEwne6yT2wFa1qPwdK2YSS8gp4Y5Zyva"), false}, // ULN default send config
	23: {solana.MustPublicKey("7a4WjyR8VZ7yZz5XJAKm39BUGn5iT9CKcv2pmG9tdXVH"), false}, // ULN treasury slot (none: the ULN program)
	24: {solana.MustPublicKey("11111111111111111111111111111111"), false},             // system program
	25: {solana.MustPublicKey("7n1YeBMVEUCJ4DscKAcpVQd6KXU7VpcEcc15ZuMcL4U3"), false}, // ULN event authority
	26: {solana.MustPublicKey("7a4WjyR8VZ7yZz5XJAKm39BUGn5iT9CKcv2pmG9tdXVH"), false}, // ULN program
	27: {solana.MustPublicKey("6doghB248px58JSSwG4qejQ46kFMW4AMj7vzJnWZHNZn"), false}, // executor program
	28: {solana.MustPublicKey("AwrbHeCyniXaQhiJZkLhgWdUCteeWSGaSN1sTfLiY7xK"), true},  // executor config
	29: {solana.MustPublicKey("8ahPGPjEbpgGaZx2NV1iG5Shj7TDwvsjkEDcGWjt94TP"), false}, // price feed program
	30: {solana.MustPublicKey("CSFsUupvJEQQd1F4SsXGACJaxQX4eropQMkGV2696eeQ"), false}, // price feed config
	31: {solana.MustPublicKey("HtEYV4xB4wvsj5fgTkcfuChYpvGYzgzwvNhgDZQNh7wW"), false}, // DVN 1 program
	32: {solana.MustPublicKey("4VDjp6XQaxoZf5RGwiPU9NR1EXSZn2TP4ATMmiSzLfhb"), true},  // DVN 1 config
	33: {solana.MustPublicKey("8ahPGPjEbpgGaZx2NV1iG5Shj7TDwvsjkEDcGWjt94TP"), false}, // DVN 1 price feed program
	34: {solana.MustPublicKey("CSFsUupvJEQQd1F4SsXGACJaxQX4eropQMkGV2696eeQ"), false}, // DVN 1 price feed config
	35: {solana.MustPublicKey("5KAALa8AEEKnW6p6AacdnqNDmGMpfhwR7AEyWs1gUvsT"), false}, // DVN 2 program
	36: {solana.MustPublicKey("7jMeX5mzXnSSKYd8DxBDP4xMnkNFZZZm5W28FWUTbwU3"), true},  // DVN 2 config
	37: {solana.MustPublicKey("8ahPGPjEbpgGaZx2NV1iG5Shj7TDwvsjkEDcGWjt94TP"), false}, // DVN 2 price feed program
	38: {solana.MustPublicKey("CSFsUupvJEQQd1F4SsXGACJaxQX4eropQMkGV2696eeQ"), false}, // DVN 2 price feed config
	39: {solana.MustPublicKey("4fs6aL12L18K5giDy9Dgxgrb3aNRYiuRV2a7JPPj3e7F"), false}, // DVN 3 program
	40: {solana.MustPublicKey("GPjyWr8vCotGuFubDpTxDxy9Vj1ZeEN4F2dwRmFiaGab"), true},  // DVN 3 config
	41: {solana.MustPublicKey("8ahPGPjEbpgGaZx2NV1iG5Shj7TDwvsjkEDcGWjt94TP"), false}, // DVN 3 price feed program
	42: {solana.MustPublicKey("CSFsUupvJEQQd1F4SsXGACJaxQX4eropQMkGV2696eeQ"), false}, // DVN 3 price feed config
	43: {solana.MustPublicKey("68C5DkFUS2BJwvjhi25s6WgJK5oi6f1TrDXPDvLK6c1b"), false}, // DVN 4 program
	44: {solana.MustPublicKey("29EKzmCscUg8mf4f5uskwMqvu2SXM8hKF1gWi1cCBoKT"), true},  // DVN 4 config
	45: {solana.MustPublicKey("8ahPGPjEbpgGaZx2NV1iG5Shj7TDwvsjkEDcGWjt94TP"), false}, // DVN 4 price feed program
	46: {solana.MustPublicKey("CSFsUupvJEQQd1F4SsXGACJaxQX4eropQMkGV2696eeQ"), false}, // DVN 4 price feed config
	47: {solana.MustPublicKey("FAAWGvgG5KNinoW4UJDF2MMU33DXeUb16tqapAgNbowR"), false}, // DVN 5 program
	48: {solana.MustPublicKey("FxFxe8j7e2xgpP9bw8LUehmz7DoQXaNFadJMEUKwBcRs"), true},  // DVN 5 config
	49: {solana.MustPublicKey("8ahPGPjEbpgGaZx2NV1iG5Shj7TDwvsjkEDcGWjt94TP"), false}, // DVN 5 price feed program
	50: {solana.MustPublicKey("CSFsUupvJEQQd1F4SsXGACJaxQX4eropQMkGV2696eeQ"), false}, // DVN 5 price feed config
	51: {solana.MustPublicKey("4Z436PCb83Ft9dMRtwZ2mK7ZQtqVhbFEC3BYna9qB7B9"), false}, // DVN 6 program
	52: {solana.MustPublicKey("HR9NQKK1ynW9NzgdM37dU5CBtqRHTukmbMKS7qkwSkHX"), true},  // DVN 6 config
	53: {solana.MustPublicKey("8ahPGPjEbpgGaZx2NV1iG5Shj7TDwvsjkEDcGWjt94TP"), false}, // DVN 6 price feed program
	54: {solana.MustPublicKey("CSFsUupvJEQQd1F4SsXGACJaxQX4eropQMkGV2696eeQ"), false}, // DVN 6 price feed config
}

const sendAccountCount = 55
