package compiler

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

// renamed prefixes the module's package-level identifiers (Instance, Host,
// New, ...) when several modules share one package.
func (c *compiler) renamed(code string) string {
	if c.prefix == "" {
		return code
	}
	p := c.prefix
	lower := strings.ToLower(p[:1]) + p[1:]
	ren := map[string]string{
		"Instance":   p,
		"Host":       p + "Host",
		"New":        "New" + p,
		"NewDynamic": "New" + p + "Dynamic",
		"dynHost":    lower + "DynHost",
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", "package p\n"+code, parser.ParseComments)
	if err != nil {
		panic("rename: " + err.Error())
	}
	skip := map[*ast.Ident]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			skip[x.Sel] = true
		case *ast.FuncDecl:
			if x.Recv != nil {
				skip[x.Name] = true
			}
		case *ast.Field:
			for _, id := range x.Names {
				skip[id] = true
			}
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok {
				skip[id] = true
			}
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || skip[id] {
			return true
		}
		if r, ok := ren[id.Name]; ok {
			id.Name = r
		} else if strings.HasPrefix(id.Name, "dataSeg") {
			id.Name = lower + "DataSeg" + id.Name[len("dataSeg"):]
		}
		return true
	})
	// keep doc comments in sync with renamed declarations
	for _, cg := range file.Comments {
		for _, cm := range cg.List {
			for _, k := range []string{"NewDynamic", "Instance", "Host", "New"} {
				if strings.HasPrefix(cm.Text, "// "+k+" ") {
					cm.Text = "// " + ren[k] + cm.Text[len("// "+k):]
					break
				}
			}
		}
	}
	var b bytes.Buffer
	printer.Fprint(&b, fset, file)
	out := b.String()
	return strings.TrimPrefix(out, "package p\n")
}
