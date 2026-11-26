package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	ctxcli "github.com/whynpc9/CtxFerry/internal/cli"
)

// Every CLI command the desktop prints for people to copy must parse with the
// CLI it names. Arguments that are not literals are replaced by a placeholder,
// so this checks command names, flag names and argument counts.
func TestDesktopCommandsMatchCLIGrammar(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var checked, spread int
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "desktopCommand" || len(call.Args) < 2 {
				return true
			}
			if call.Ellipsis.IsValid() {
				spread++
				return true
			}
			args := []string{}
			for _, arg := range call.Args[1:] {
				value := "placeholder"
				if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					value, _ = strconv.Unquote(lit.Value)
				}
				args = append(args, value)
			}
			args = append(args, "--data-dir", "/data")
			if err := ctxcli.Validate(args); err != nil {
				t.Errorf("%s: %v: %v", fset.Position(call.Pos()), args, err)
			}
			checked++
			return true
		})
	}
	if checked == 0 {
		t.Fatal("found no desktop commands")
	}
	// Commands built from a slice are covered by their builder's own test.
	if spread != 2 {
		t.Fatalf("%d desktop commands are built from slices; give each a builder test", spread)
	}
}

// An implicit default Profile leaves no selected Profile; the handoff must
// never pass an empty --profile.
func TestTrustPreviewCommandMatchesCLIGrammar(t *testing.T) {
	for _, profile := range []string{"", "default", "staging"} {
		args := trustPreviewArgs("8d7f0a7e-2f5b-4c4f-9a59-6f1c3a9d2e10", profile, "checkout")
		for i, arg := range args {
			if arg == "--profile" && args[i+1] == "" {
				t.Errorf("%v: empty --profile", args)
			}
		}
		if err := ctxcli.Validate(append(args, "--data-dir", "/data")); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestJoinCommandMatchesCLIGrammar(t *testing.T) {
	for _, subscriptions := range []string{"", "alpha, beta"} {
		args := append(joinArgs("git@github.com:owner/repo.git", subscriptions), "--data-dir", "/data")
		if err := ctxcli.Validate(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}
