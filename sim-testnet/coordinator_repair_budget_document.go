package main

import (
	"errors"
	"reflect"
)

// The original repair command retained the full audit document and signed its
// exact SHA256 alongside a smaller executable budget projection. Keep this
// typed document separate: adding its fields to coordinatorRepairBudget would
// change the original request's canonical hash and invalidate its signature.
// These observations confer no authority beyond that signed projection.
type coordinatorRepairBudgetDocument struct {
	coordinatorRepairBudget
	ObservedAt string `json:"observed_at,omitempty"`
	JournalSequence uint64 `json:"journal_sequence,omitempty"`
	AvailableAfterRepairMaxWei string `json:"available_after_repair_max_wei,omitempty"`
	VerificationScope string `json:"verification_scope,omitempty"`
	ActualPaidFeeTotalWei *string `json:"actual_paid_fee_total_wei,omitempty"`
	ActualPaidFeeTotalStatus string `json:"actual_paid_fee_total_status,omitempty"`
	OperatorSignedMaxWei map[string]string `json:"operator_signed_max_wei,omitempty"`
	UnsignedPreparedIntents []coordinatorRepairBudgetUnsignedIntent `json:"unsigned_prepared_intents,omitempty"`
	FutureTransactionScope string `json:"future_transaction_scope,omitempty"`
	RetirementReserveWei string `json:"retirement_reserve_wei,omitempty"`
	RetirementDebitedWei string `json:"retirement_debited_wei,omitempty"`
	NewFundingWei string `json:"new_funding_wei,omitempty"`
	EvidenceSHA256 map[string]string `json:"evidence_sha256,omitempty"`
}

type coordinatorRepairBudgetUnsignedIntent struct {
	Operator uint64 `json:"operator"`
	IntentID string `json:"intent_id"`
	FromAddress string `json:"from_address"`
	ToAddress string `json:"to_address"`
	Nonce uint64 `json:"nonce"`
	Status string `json:"status"`
	AttemptCount uint64 `json:"attempt_count"`
	CurrentTxHash *string `json:"current_tx_hash"`
	CreateTime string `json:"create_time"`
	UpdateTime string `json:"update_time"`
	HasSignedAttempt bool `json:"has_signed_attempt"`
	FeeBoundWei *string `json:"fee_bound_wei"`
	FeeBoundStatus string `json:"fee_bound_status"`
}

func validateCoordinatorRepairBudgetDocument(raw []byte, request coordinatorRepairRequest) error {
	if !validCoordinatorRepairSHA256(request.BudgetSHA256) || bytesSHA256(raw) != "sha256:"+request.BudgetSHA256 {
		return errors.New("coordinator repair budget bytes changed")
	}
	var document coordinatorRepairBudgetDocument
	if err := decodeExactCoordinatorRepairJSON(raw, &document); err != nil {
		return err
	}
	if !reflect.DeepEqual(document.coordinatorRepairBudget, request.Budget) {
		return errors.New("coordinator repair budget differs from signed request")
	}
	return nil
}
