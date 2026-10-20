package compiler

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

// pruneDead removes statements that can never execute because they follow
// a statement that ends control flow (return, goto, break, continue, panic,
// or a terminating if/for/switch), so that generated code is free of
// "unreachable code" vet diagnostics. Labeled statements are kept since
// they may be goto targets.
func pruneDead(body string) string {
	src := "package p\nfunc _() {\n" + body + "\n}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return body
	}
	fd := file.Decls[0].(*ast.FuncDecl)
	changed := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BlockStmt:
			if l, ok := pruneList(x.List); ok {
				x.List = l
				changed = true
			}
		case *ast.CaseClause:
			if l, ok := pruneList(x.Body); ok {
				x.Body = l
				changed = true
			}
		}
		return true
	})
	if !changed {
		return body
	}
	var b bytes.Buffer
	for _, st := range fd.Body.List {
		printer.Fprint(&b, fset, st)
		b.WriteByte('\n')
	}
	return b.String()
}

func pruneList(list []ast.Stmt) ([]ast.Stmt, bool) {
	for i, st := range list {
		if !endsFlow(st) {
			continue
		}
		j := i + 1
		for j < len(list) {
			if _, ok := list[j].(*ast.LabeledStmt); ok {
				break
			}
			j++
		}
		if j == i+1 {
			continue
		}
		out := append(append([]ast.Stmt{}, list[:i+1]...), list[j:]...)
		r, _ := pruneList(out)
		return r, true
	}
	return list, false
}

func endsFlow(s ast.Stmt) bool {
	switch x := s.(type) {
	case *ast.BranchStmt:
		return x.Tok != token.FALLTHROUGH
	}
	return isTerminating(s, "")
}

// isTerminating implements the Go specification's terminating statements.
func isTerminating(s ast.Stmt, label string) bool {
	switch x := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return x.Tok == token.GOTO || x.Tok == token.FALLTHROUGH
	case *ast.ExprStmt:
		if c, ok := x.X.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	case *ast.BlockStmt:
		return len(x.List) > 0 && isTerminating(x.List[len(x.List)-1], "")
	case *ast.IfStmt:
		return x.Else != nil && isTerminating(x.Body, "") && isTerminating(x.Else, "")
	case *ast.LabeledStmt:
		return isTerminating(x.Stmt, x.Label.Name)
	case *ast.ForStmt:
		return x.Cond == nil && !hasBreak(x.Body, label, true)
	case *ast.SwitchStmt:
		if hasBreak(x.Body, label, true) {
			return false
		}
		hasDefault := false
		for _, c := range x.Body.List {
			cc := c.(*ast.CaseClause)
			if cc.List == nil {
				hasDefault = true
			}
			if len(cc.Body) == 0 || !isTerminating(cc.Body[len(cc.Body)-1], "") {
				return false
			}
		}
		return hasDefault
	}
	return false
}

// hasBreak reports whether n contains a break that targets the enclosing
// statement (unlabeled at this nesting level, or labeled with label).
func hasBreak(n ast.Node, label string, top bool) bool {
	found := false
	ast.Inspect(n, func(m ast.Node) bool {
		if found {
			return false
		}
		switch x := m.(type) {
		case *ast.BranchStmt:
			if x.Tok == token.BREAK {
				if x.Label == nil && top {
					found = true
				} else if x.Label != nil && x.Label.Name == label && label != "" {
					found = true
				}
			}
		case *ast.ForStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.RangeStmt:
			if m != n {
				if label != "" && hasBreak(m, label, false) {
					found = true
				}
				return false
			}
		case *ast.FuncLit:
			return false
		}
		return true
	})
	return found
}

var _ = strings.TrimSpace
