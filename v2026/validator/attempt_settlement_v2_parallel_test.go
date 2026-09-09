//go:build linux || darwin

// Private fixture families enter the bounded Go parallel pool before their
// expensive signed-disk setup. All production assertions remain unchanged.
package validator

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

// A late or missing Parallel call leaves the expensive prefix serialized.
// Inspect executable syntax so comments and similarly named calls cannot pass.
func verifySettlementFixtureParallelAdmission(name string, source []byte) (int, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") {
			continue
		}
		count++
		if function.Body == nil || len(function.Body.List) == 0 {
			return count, fmt.Errorf("%s has no parallel admission", function.Name.Name)
		}
		expression, ok := function.Body.List[0].(*ast.ExprStmt)
		if !ok {
			return count, fmt.Errorf("%s performs serial setup before admission", function.Name.Name)
		}
		call, ok := expression.X.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return count, fmt.Errorf("%s has no first parallel call", function.Name.Name)
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Parallel" {
			return count, fmt.Errorf("%s does not first enter the parallel pool", function.Name.Name)
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok || receiver.Name != "t" {
			return count, fmt.Errorf("%s admits a different test owner", function.Name.Name)
		}
	}
	if count == 0 {
		return 0, errors.New("parallel fixture source has no test roots")
	}
	return count, nil
}

// The exact audited original files are the regression input, not a convenient
// sample. The pre-fix serial sources deterministically fail this admission.
func TestAttemptSettlementRuntimeV2PrivateFixturesEnterParallelBeforeWork(t *testing.T) {
	t.Parallel()
	for _, source := range []struct {
		path  string
		roots int
	}{
		{path: "attempt_settlement_v2_runtime_test.go", roots: 17},
		{path: "attempt_settlement_v2_legacy_history_test.go", roots: 3},
		{path: "release_activation_history_v2_test.go", roots: 14},
	} {
		encoded, err := os.ReadFile(source.path)
		if err != nil {
			t.Fatal(err)
		}
		count, err := verifySettlementFixtureParallelAdmission(source.path, encoded)
		if err != nil || count != source.roots {
			t.Fatalf("%s serial prefix or root census changed: roots=%d error=%v", source.path, count, err)
		}
	}
}

// The guard distinguishes actual first admission from late work, comments,
// an unrelated receiver and an empty selection without relying on elapsed time.
func TestAttemptSettlementRuntimeV2ParallelAdmissionGuardRejectsSerialPrefixes(t *testing.T) {
	t.Parallel()
	positive := []byte("package fixture\nfunc TestOwned(t *testing.T) { t.Parallel(); fixture() }\n")
	if count, err := verifySettlementFixtureParallelAdmission("fixture_test.go", positive); err != nil || count != 1 {
		t.Fatalf("first admission was not recognized: %d/%v", count, err)
	}
	for _, body := range []string{
		"fixture(); t.Parallel()",
		"fixture()",
		"// t.Parallel()\n fixture()",
		"other.Parallel(); fixture()",
		"t.Parallel(1); fixture()",
		"",
	} {
		source := []byte("package fixture\nfunc TestOwned(t *testing.T) {\n" + body + "\n}\n")
		if _, err := verifySettlementFixtureParallelAdmission("fixture_test.go", source); err == nil {
			t.Fatalf("serial or foreign admission passed: %q", body)
		}
	}
	if _, err := verifySettlementFixtureParallelAdmission("fixture_test.go", []byte("package fixture\n")); err == nil {
		t.Fatal("empty test census was admitted")
	}
}

// Both real transactions reach their first physical journal write before
// either proceeds. The barrier proves overlap; no sleep or elapsed-time guess
// substitutes for genuine signed M8 replay and private filesystem ownership.
func TestAttemptSettlementRuntimeV2ParallelFixturesKeepPhysicalOwnersDistinct(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type operation struct {
		fixture *attemptSettlementRuntimeV2TestFixture
		options AttemptSettlementRuntimeV2Options
		ready   chan struct{}
		done    chan struct{}
		closure *AttemptSettlementClosureV2
		err     error
	}
	operations := make([]*operation, 2)
	var directories []os.FileInfo
	for index := range operations {
		fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
		fixture.trails(t, 0, 1, 0)
		fixture.trails(t, 1, 1, 0)
		paths := []string{fixture.coordinator}
		for _, participant := range fixture.participants {
			paths = append(paths, participant.StateDir)
		}
		for _, path := range paths {
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() {
				t.Fatalf("private physical owner missing: %s: %v", path, err)
			}
			for _, previous := range directories {
				if os.SameFile(info, previous) {
					t.Fatal("separate fixture roles share one physical owner")
				}
			}
			directories = append(directories, info)
		}
		operations[index] = &operation{fixture: fixture, options: fixture.options(t), ready: make(chan struct{}), done: make(chan struct{})}
	}
	release := make(chan struct{})
	for _, operation := range operations {
		go func() {
			defer close(operation.done)
			physical := attemptSettlementV2PhysicalIO()
			write := physical.writeJournal
			writes := 0
			physical.writeJournal = func(directory *attemptPrivateDirectory, name string, data []byte) error {
				writes++
				if writes != 1 {
					return errors.New("parallel fixture journal was written more than once")
				}
				close(operation.ready)
				select {
				case <-release:
					return write(directory, name, data)
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			fixture := operation.fixture
			operation.closure, operation.err = advanceAttemptSettlementEpochV2(ctx, fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, operation.options, physical, true)
		}()
	}
	// Join every started worker even if an admission fails before the barrier.
	defer func() {
		cancel()
		for _, operation := range operations {
			<-operation.done
		}
	}()
	for _, operation := range operations {
		select {
		case <-operation.ready:
		case <-operation.done:
			t.Fatalf("transaction ended before concurrent journal admission: %v", operation.err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(release)
	for _, operation := range operations {
		<-operation.done
		if operation.err != nil || operation.closure == nil || len(operation.closure.Transitions) != 2 {
			t.Fatalf("actual concurrent complete settlement failed: %v", operation.err)
		}
		for _, transition := range operation.closure.Transitions {
			if transition.Cut.RecordCount != 8 || transition.Cut.CompleteCount != 1 {
				t.Fatal("concurrent fixture lost an original complete M8 trail")
			}
		}
		fixture := operation.fixture
		retained, verified, err := ReadAttemptSettlementClosureV2(t.Context(), fixture.coordinator, 42, fixture.options(t).Authority)
		if err != nil || !reflect.DeepEqual(retained, operation.closure) || len(verified.Operators) != 2 {
			t.Fatalf("independent physical closure replay differs: %v", err)
		}
		if _, err := os.Lstat(attemptSettlementTransactionV2Path(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("concurrent transaction retained its journal: %v", err)
		}
	}
}
