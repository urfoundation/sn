package main

// The durable shutdown tests use the actual RunRelease owner operation; these
// guards pin its returned error and the real final-save callback route.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestReleaseShutdownPublicRuntimeReturnsOwnedWorkerResult(t *testing.T) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "../validator/release_run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "RunRelease" || function.Body == nil {
			continue
		}
		for _, statement := range function.Body.List {
			result, ok := statement.(*ast.ReturnStmt)
			if !ok || len(result.Results) != 1 {
				continue
			}
			call, ok := result.Results[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			name, ok := call.Fun.(*ast.Ident)
			if ok && name.Name == "runReleaseOperatorWorkers" {
				matched = true
			}
		}
	}
	if !matched {
		t.Fatal("public release runtime does not return the actual owned worker lifecycle result")
	}
	for _, check := range []struct{ path, function, callee string }{
		{path: "../validator/release_run.go", function: "startReleaseOperatorWithAdmission", callee: "newReleaseOperatorClose"},
		{path: "../validator/release_shutdown.go", function: "newReleaseOperatorClose", callee: "Save"},
		{path: "../validator/release_shutdown.go", function: "newReleaseOperatorClose", callee: "closeResources"},
		{path: "../validator/release_shutdown.go", function: "runReleaseOperatorWorkers", callee: "reportReleaseTrailEngineError"},
		{path: "../validator/release_shutdown.go", function: "runReleaseOperatorWorkers", callee: "Wait"},
		{path: "../validator/release_shutdown.go", function: "runReleaseOperatorWorkers", callee: "close"},
		{path: "../validator/release_steer.go", function: "Run", callee: "runReleaseSteeringLoop"},
		{path: "../validator/release_steer.go", function: "Run", callee: "SubmitOnce"},
		{path: "../validator/release_steer.go", function: "runReleaseSteeringLoop", callee: "runReleaseSteeringLoopWithWait"},
		{path: "../validator/release_steer.go", function: "SubmitOnce", callee: "recordReleasePendingError"},
		{path: "../validator/release_steer.go", function: "reconcilePending", callee: "recordReleasePendingError"},
		{path: "../validator/release_run.go", function: "RunRelease", callee: "closeReleaseAttemptStates"},
		{path: "../validator/release_shutdown.go", function: "closeReleaseAttemptStates", callee: "Close"},
		{path: "../validator/release_run.go", function: "loadReleaseAttemptStateWithObserver", callee: "Close"},
		{path: "../validator/intent.go", function: "Begin", callee: "validateSteeringIntentLifecycle"},
		{path: "../validator/intent.go", function: "update", callee: "validateSteeringIntentLifecycle"},
	} {
		if !releaseClosureFunctionCalls(t, check.path, check.function)[check.callee] {
			t.Errorf("%s omits actual shutdown call %s", check.function, check.callee)
		}
	}
	if releaseClosureFunctionCalls(t, "../validator/release_shutdown_inner.go", "recordReleasePendingError")["update"] {
		t.Fatal("uncertain pending error handler rewrites the restart authority")
	}
}
