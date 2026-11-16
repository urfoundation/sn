// A transaction pool can answer one exact signed extrinsic in ways that leave
// its fate to the chain: its account nonce is already taken, by another
// transaction or by these very bytes, or it is still ahead of the account's,
// or the watch ends without a verdict. Only a finalized read decides those, so
// they get one closed type that an owner keeping the signed bytes can wait on.
// Every other refusal keeps its original error.
//
// The SN25 validator and its weight fallback sign with one hotkey. On
// 2026-11-08 the fallback's commit took the nonce of the validator's signed
// commit, and the pool's refusals of the validator's rebroadcast spent its
// steering failure budget: the validator panicked and lost 34 epochs.
package crv4

import (
	"fmt"
	"reflect"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// What the pool's answer says about the extrinsic and its nonce.
type SubmissionOutcome uint8

const (
	// Another transaction holds the nonce in the pool or used it in a block,
	// or these bytes used it: substrate's "Transaction is outdated" (1010),
	// a pooled transaction with the same nonce outranks it (1014), or one
	// replaced it during the watch (usurped).
	SubmissionNonceTaken SubmissionOutcome = iota + 1
	// The nonce is ahead of the account's: "Transaction will be valid in the
	// future" (1010), or a pool that refuses future transactions (1021).
	SubmissionNonceAhead
	// The pool already holds these exact bytes (1013), or removed them
	// recently, after their inclusion, replacement or invalidation, and
	// refuses them for a while (1012).
	SubmissionBytesKnown
	// The watch ended before finality without a chain verdict: invalid,
	// dropped, retracted, or the node's finality watcher timed out.
	SubmissionWatchEnded
)

// Substrate's author RPC error codes (sc-rpc-api, author/error.rs) and its text
// for InvalidTransaction::Stale and ::Future, which a 1010 refusal carries as
// its data.
const (
	authorPoolInvalidTransaction = 1010
	authorPoolTemporarilyBanned  = 1012
	authorPoolAlreadyImported    = 1013
	authorPoolTooLowPriority     = 1014
	authorPoolFutureRejected     = 1021

	invalidTransactionStale  = "Transaction is outdated"
	invalidTransactionFuture = "Transaction will be valid in the future"
)

// One exact extrinsic whose submission or watch left its outcome open. It
// carries values only and wraps nothing, so no classifier opens another graph.
type SubmissionUnresolvedError struct {
	ExtrinsicHash types.Hash
	Outcome       SubmissionOutcome
	// The author RPC's refusal: code, message and string data. Code is zero
	// when a watch status ended the observation.
	Code    int
	Message string
	Data    string
	// The watch status that ended the observation, or empty for a refusal.
	Status string
}

func (self *SubmissionUnresolvedError) Error() string {
	if self == nil {
		return "crv4: submission outcome is unresolved"
	}
	if self.Status != "" {
		return fmt.Sprintf("crv4: extrinsic %s watch ended before finality (%s); finality decides its outcome", self.ExtrinsicHash.Hex(), self.Status)
	}
	text := fmt.Sprintf("crv4: submit/watch %s refused by the pool (%d %s", self.ExtrinsicHash.Hex(), self.Code, self.Message)
	if self.Data != "" {
		text += ": " + self.Data
	}
	return text + "); finality decides its outcome"
}

// The gsrpc client returns the node's JSON-RPC error object as its private
// jsonError type. It is recognized by package path and type name and read
// through its exported fields; none of its methods is called. Only the exact
// codes, and for 1010 the exact data, above qualify.
func submissionRefusal(hash types.Hash, err error) *SubmissionUnresolvedError {
	value := reflect.ValueOf(err)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return nil
	}
	object := value.Elem()
	kind := object.Type()
	if object.Kind() != reflect.Struct || kind.PkgPath() != "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc" || kind.Name() != "jsonError" {
		return nil
	}
	code, message, data := object.FieldByName("Code"), object.FieldByName("Message"), object.FieldByName("Data")
	if !code.IsValid() || code.Kind() != reflect.Int || !message.IsValid() || message.Kind() != reflect.String || !data.IsValid() || data.Kind() != reflect.Interface {
		return nil
	}
	refusal := &SubmissionUnresolvedError{ExtrinsicHash: hash, Code: int(code.Int()), Message: message.String()}
	if !data.IsNil() && data.Elem().Kind() == reflect.String {
		refusal.Data = data.Elem().String()
	}
	switch {
	case refusal.Code == authorPoolInvalidTransaction && refusal.Data == invalidTransactionStale, refusal.Code == authorPoolTooLowPriority:
		refusal.Outcome = SubmissionNonceTaken
	case refusal.Code == authorPoolInvalidTransaction && refusal.Data == invalidTransactionFuture, refusal.Code == authorPoolFutureRejected:
		refusal.Outcome = SubmissionNonceAhead
	case refusal.Code == authorPoolAlreadyImported, refusal.Code == authorPoolTemporarilyBanned:
		refusal.Outcome = SubmissionBytesKnown
	default:
		return nil
	}
	return refusal
}

// A watch status that ends observation without finality.
func submissionWatchEnded(hash types.Hash, status types.ExtrinsicStatus) *SubmissionUnresolvedError {
	ended := &SubmissionUnresolvedError{ExtrinsicHash: hash, Outcome: SubmissionWatchEnded}
	switch {
	case status.IsUsurped:
		ended.Outcome, ended.Status = SubmissionNonceTaken, "usurped by "+status.AsUsurped.Hex()
	case status.IsInvalid:
		ended.Status = "invalid"
	case status.IsDropped:
		ended.Status = "dropped"
	case status.IsRetracted:
		ended.Status = "retracted from " + status.AsRetracted.Hex()
	case status.IsFinalityTimeout:
		ended.Status = "finality timeout at " + status.AsFinalityTimeout.Hex()
	default:
		return nil
	}
	return ended
}
