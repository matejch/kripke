package purity

import (
	"sort"
	"strings"
)

// matchSegment reports whether path equals prefix or lies under it on a path-segment boundary.
func matchSegment(path, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// matchAny reports whether path segment-matches any of the prefixes.
func matchAny(path string, prefixes []string) bool {
	for _, pre := range prefixes {
		if pre != "" && matchSegment(path, pre) {
			return true
		}
	}
	return false
}

// denied reports whether the import path is forbidden under the policy.
func (p Policy) denied(path string) bool {
	if matchAny(path, p.AllowImports) {
		return false
	}
	return matchAny(path, defaultDeny) || matchAny(path, p.DenyImports)
}

// reach records how a same-module package was reached from a target.
type reach struct {
	root  string
	chain []string
}

// walkModule finds every same-module package reachable from the targets, with the first import chain to each.
func walkModule(pkgs map[string]*listPackage, targets []string) (map[string]reach, []string) {
	seen := map[string]reach{}
	var order []string
	sorted := append([]string(nil), targets...)
	sort.Strings(sorted)
	for _, t := range sorted {
		if _, ok := seen[t]; ok {
			continue
		}
		mod := pkgs[t].modulePath()
		seen[t] = reach{root: t}
		order = append(order, t)
		queue := []string{t}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, imp := range pkgs[cur].Imports {
				dep, ok := pkgs[imp]
				if !ok || dep.Standard || mod == "" || dep.modulePath() != mod {
					continue
				}
				if _, ok := seen[imp]; ok {
					continue
				}
				prev := seen[cur]
				chain := append(append([]string(nil), prev.chain...), imp)
				seen[imp] = reach{root: t, chain: chain}
				order = append(order, imp)
				queue = append(queue, imp)
			}
		}
	}
	return seen, order
}

// importMsg describes a denied import, naming the chain for transitive hits.
func importMsg(path string, r reach) string {
	if len(r.chain) == 0 {
		return "imports " + path
	}
	return "imports " + path + " via " + strings.Join(r.chain, " -> ")
}
