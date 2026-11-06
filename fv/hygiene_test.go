package fv

import (
	"strings"
	"testing"
)

func TestHygieneWarnsOnMapsAndInterfaces(t *testing.T) {
	w := hygiene(&keyed{})
	if len(w) != 2 || !strings.Contains(w[0], "M is a map") || !strings.Contains(w[1], "Any is an interface") {
		t.Fatalf("got %q", w)
	}
}

func TestHygieneIsQuietForPlainStructs(t *testing.T) {
	if w := hygiene(&cnt{}); len(w) != 0 {
		t.Fatalf("got %q", w)
	}
}
