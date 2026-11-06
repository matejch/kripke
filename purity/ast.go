package purity

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

// fileChecker applies the import and AST rules to one parsed file.
type fileChecker struct {
	fset     *token.FileSet
	pkg      *listPackage
	reach    reach
	policy   Policy
	names    map[string]string
	dotTime  bool
	findings []Finding
}

// checkPackage parses the package's non-test Go files and returns their findings.
func checkPackage(pkg *listPackage, r reach, p Policy) ([]Finding, error) {
	fset := token.NewFileSet()
	var out []Finding
	files := append(append([]string(nil), pkg.GoFiles...), pkg.CgoFiles...)
	for _, name := range files {
		f, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		c := &fileChecker{fset: fset, pkg: pkg, reach: r, policy: p, names: map[string]string{}}
		c.checkFile(f)
		out = append(out, c.findings...)
	}
	return out, nil
}

// add records a finding at pos.
func (c *fileChecker) add(pos token.Pos, rule, msg string) {
	at := c.fset.Position(pos)
	c.findings = append(c.findings, Finding{Pkg: c.reach.root, File: at.Filename, Line: at.Line, Col: at.Column, Rule: rule, Msg: msg})
}

// checkFile runs every rule over f.
func (c *fileChecker) checkFile(f *ast.File) {
	c.checkImports(f)
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "init" {
				c.add(d.Pos(), "init", "func init is not allowed")
			}
		case *ast.GenDecl:
			if d.Tok == token.VAR {
				c.checkGlobals(d)
			}
		}
	}
	ast.Inspect(f, c.visit)
}

// checkImports records denied imports and the local names of time, unsafe, errors and fmt.
func (c *fileChecker) checkImports(f *ast.File) {
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		local := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			local = path[i+1:]
		}
		if spec.Name != nil {
			local = spec.Name.Name
		}
		switch {
		case local == ".":
			if path == "time" {
				c.dotTime = true
			}
		case local != "_":
			c.names[local] = path
		}
		if c.policy.denied(path) {
			c.add(spec.Pos(), "import", importMsg(path, c.reach))
		}
	}
}

// pkgOf returns the import path that the identifier x names in this file, or "".
func (c *fileChecker) pkgOf(x ast.Expr) string {
	id, ok := x.(*ast.Ident)
	if !ok {
		return ""
	}
	return c.names[id.Name]
}

// checkGlobals reports package-level variables other than blanks, sentinel errors and allowlisted names.
func (c *fileChecker) checkGlobals(d *ast.GenDecl) {
	for _, s := range d.Specs {
		vs := s.(*ast.ValueSpec)
		for i, name := range vs.Names {
			if name.Name == "_" || c.policy.allowsGlobal(c.pkg.ImportPath, name.Name) {
				continue
			}
			if len(vs.Values) == len(vs.Names) && c.isSentinel(vs.Values[i]) {
				continue
			}
			c.add(name.Pos(), "global", "package-level var "+name.Name+" is mutable state")
		}
	}
}

// isSentinel reports whether e is a call to errors.New or fmt.Errorf.
func (c *fileChecker) isSentinel(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch c.pkgOf(sel.X) {
	case "errors":
		return sel.Sel.Name == "New"
	case "fmt":
		return sel.Sel.Name == "Errorf"
	}
	return false
}

// visit applies the statement and expression rules to n.
func (c *fileChecker) visit(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.GoStmt:
		c.add(x.Pos(), "go", "go statement starts a goroutine")
	case *ast.SendStmt:
		c.add(x.Pos(), "chan", "channel send")
	case *ast.UnaryExpr:
		if x.Op == token.ARROW {
			c.add(x.Pos(), "chan", "channel receive")
		}
	case *ast.ChanType:
		c.add(x.Pos(), "chan", "channel type")
	case *ast.SelectStmt:
		c.add(x.Pos(), "select", "select statement")
	case *ast.SelectorExpr:
		if c.pkgOf(x.X) == "unsafe" {
			c.add(x.Pos(), "unsafe", "use of unsafe."+x.Sel.Name)
		}
	case *ast.CallExpr:
		c.checkClock(x)
	}
	return true
}

// checkClock reports calls that read or wait on the wall clock.
func (c *fileChecker) checkClock(call *ast.CallExpr) {
	var name string
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		if c.pkgOf(fn.X) != "time" {
			return
		}
		name = fn.Sel.Name
	case *ast.Ident:
		if !c.dotTime {
			return
		}
		name = fn.Name
	default:
		return
	}
	if !clockFuncs[name] || c.policy.allowsCall("time."+name) {
		return
	}
	c.add(call.Pos(), "clock", "call to time."+name+" reads the clock")
}
