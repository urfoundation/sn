package ledger

// Check 11 with relay rows (WHITEPAPER.md §8.6.2, §8.6.4): the validator reads
// a covered epoch's relay chunks, holds them to the relay rules, and
// recomputes the share rows from the census and the relay rows together. The
// relay bytes themselves, like the census, are the operator's attestation: a
// relay list forged consistently, with its share rows and accounts rebuilt,
// passes.

import (
	"context"
	"errors"
	"testing"
)

// Epoch 12's relay rows: x21 relayed 30 bytes and x23 10, so R = 40. Z' = 600
// splits 300 by unit, units 1 and 2 taking 150 each (x21 and x22 75 each, x23
// 150), and 300 by relay bytes, x21 225 and x23 75.
func relay12() []RelayRow {
	return []RelayRow{relayRow(21, 30), relayRow(23, 10)}
}

// Publishes covered epoch i's relay rows.
func (pub *publication) setRelay(i int, rows []RelayRow) {
	pub.header.Epochs[i].Extenders.Relay = pub.put(EncodeRelayChunks(rows))
}

// The relay rows enter the recomputed split: an honest root passes with its
// shares, and every relay change the operator did not carry into the share
// rows, a relay row it cannot list, or a relay chunk withheld or altered, is
// refused or demanded. A component's relay list is part of its fixed inputs.
func TestVerifyExtendersRelay(t *testing.T) {
	s := newScenario(t)
	honest := s.buildExtenders(t, nil, &ExtenderInput{Bytes: z12, Census: census12(), Relay: relay12()})
	if got := honest.Epochs[1].Extenders.Rows; len(got) != 3 || got[0] != shareRow(21, 9, 75+225) || got[1] != shareRow(22, 10, 75) || got[2] != shareRow(23, 11, 150+75) {
		t.Fatalf("epoch 12 shares %+v", got)
	}
	if split := honest.Epochs[1].Extenders.ExtenderSplit; split.RelayWeight != 300 || split.UnitWeight != 300 || split.RelayBytes != 40 || split.Relaying != 2 {
		t.Fatalf("epoch 12 split %+v", split)
	}
	s.custody = cloneCustodyAfterF(t, s)
	expectPass(t, mustVerify(t, s.extendersFrom(honest.Commit, honest.Objects, 12)))

	run := func(name string, mutate func(pub *publication)) *Result {
		t.Helper()
		s.custody = cloneCustodyAfterF(t, s)
		pub := publish(t, honest)
		mutate(pub)
		return mustVerify(t, s.extendersFrom(pub.seal(t), pub.objects, 12))
	}
	expectFailure(t, run("relay bytes raised without the shares", func(pub *publication) {
		pub.setRelay(1, []RelayRow{relayRow(21, 31), relayRow(23, 10)})
	}), CheckExtenders, ActionRefuse, ErrExtenderRows)
	expectFailure(t, run("relay rows dropped without the shares", func(pub *publication) { pub.header.Epochs[1].Extenders.Relay = []Ref{} }), CheckExtenders, ActionRefuse, ErrExtenderRows)
	expectFailure(t, run("a relay row of a client the census lacks", func(pub *publication) {
		pub.setRelay(1, []RelayRow{relayRow(21, 30), relayRow(23, 10), relayRow(25, 1)})
	}), CheckExtenders, ActionRefuse, ErrRelayRows)
	expectFailure(t, run("relay rows unsorted", func(pub *publication) {
		pub.setRelay(1, []RelayRow{relayRow(23, 10), relayRow(21, 30)})
	}), CheckExtenders, ActionRefuse, ErrRelayRows)
	expectFailure(t, run("a relay chunk cut short", func(pub *publication) {
		chunk := EncodeRelayChunks(relay12())[0]
		pub.header.Epochs[1].Extenders.Relay = pub.put([][]byte{chunk[:len(chunk)-1]})
	}), CheckChunks, ActionRefuse, ErrChunkLength)
	relayRef := honest.Epochs[1].Extenders.Relay[0]
	expectFailure(t, run("relay withheld", func(pub *publication) { delete(pub.objects, relayRef) }), CheckFetch, ActionDemand, ErrObjectUnavailable)
	expectFailure(t, run("relay served altered", func(pub *publication) {
		pub.objects[relayRef] = EncodeRelayChunks([]RelayRow{relayRow(21, 31), relayRow(23, 10)})[0]
	}), CheckChunks, ActionDemand, ErrChunkHash)

	// Relay rows forged consistently, the shares and accounts rebuilt from
	// them, are the operator's attestation and pass.
	s.custody = cloneCustodyAfterF(t, s)
	forged := s.buildExtenders(t, nil, &ExtenderInput{Bytes: z12, Census: census12(), Relay: []RelayRow{relayRow(22, 1)}})
	expectPass(t, mustVerify(t, s.extendersFrom(forged.Commit, forged.Objects, 12)))

	// Once custody holds the component, a replacement root with other relay
	// rows lists another component, and the builder holds a rebuilt epoch to
	// its recorded relay chunks.
	s.custody = cloneCustodyAfterF(t, s)
	expectPass(t, mustVerify(t, s.extendersFrom(honest.Commit, honest.Objects, 12)))
	held, ok, err := s.custody.ValidExtenders(context.Background(), testNoID, 12)
	if err != nil || !ok || !held.Equal(honest.Epochs[1].Extenders.Header()) || len(held.Relay) != 1 {
		t.Fatalf("custody holds %+v %v %v", held, ok, err)
	}
	other := s.buildExtenders(t, nil, &ExtenderInput{Bytes: z12, Census: census12(), Relay: []RelayRow{relayRow(21, 30)}})
	expectFailure(t, mustVerify(t, s.extendersFrom(other.Commit, other.Objects, 12)), CheckFixedInputs, ActionRefuse, ErrFixedInputs)
	recorded := honest.Epochs[1].Extenders
	rebuild := func(relay []RelayRow) error {
		_, err := buildOver(s.f, []EpochInput{{Epoch: 11, Rows: epoch11Rows()}, {Epoch: 12, Rows: epoch12Rows(), Extenders: &ExtenderInput{
			Bytes: z12, Census: census12(), Relay: relay, Chunks: recorded.Census, RelayChunks: recorded.Relay,
		}}}, s.funded)
		return err
	}
	if err := rebuild([]RelayRow{relayRow(21, 30)}); !errors.Is(err, ErrFixedInputs) {
		t.Fatalf("rebuilt other relay rows: %v", err)
	}
	if err := rebuild(relay12()); err != nil {
		t.Fatalf("rebuilt the recorded relay rows: %v", err)
	}
	// An epoch recorded with no relay rows rebuilds with none.
	none := s.buildExtenders(t, nil, &ExtenderInput{Bytes: z12, Census: census12()})
	if len(none.Epochs[1].Extenders.Relay) != 0 {
		t.Fatalf("relay chunks %v for no relay rows", none.Epochs[1].Extenders.Relay)
	}
	if _, err := buildOver(s.f, []EpochInput{{Epoch: 11, Rows: epoch11Rows()}, {Epoch: 12, Rows: epoch12Rows(), Extenders: &ExtenderInput{
		Bytes: z12, Census: census12(), Chunks: none.Epochs[1].Extenders.Census,
	}}}, s.funded); err != nil {
		t.Fatalf("rebuilt an epoch recorded without relay rows: %v", err)
	}
}
