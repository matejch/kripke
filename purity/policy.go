package purity

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// Policy adjusts the default rules; it is usually read from purity.yaml at the module root.
type Policy struct {
	AllowImports []string `yaml:"allow_imports"`
	DenyImports  []string `yaml:"deny_imports"`
	AllowGlobals []string `yaml:"allow_globals"`
	AllowCalls   []string `yaml:"allow_calls"`
}

var defaultDeny = []string{
	"context",
	"crypto/rand",
	"database/sql",
	"io/fs",
	"log",
	"log/slog",
	"math/rand",
	"math/rand/v2",
	"net",
	"os",
	"os/exec",
	"os/signal",
	"plugin",
	"reflect",
	"runtime",
	"sync",
	"sync/atomic",
	"syscall",
	"testing",
	"unsafe",
	"github.com/segmentio/kafka-go",
	"github.com/IBM/sarama",
	"github.com/Shopify/sarama",
	"github.com/confluentinc/confluent-kafka-go",
	"github.com/twmb/franz-go",
	"github.com/lib/pq",
	"github.com/jackc/pgx",
	"github.com/jackc/pgx/v5",
	"google.golang.org/grpc",
}

var clockFuncs = map[string]bool{
	"Now":       true,
	"Since":     true,
	"Until":     true,
	"Tick":      true,
	"After":     true,
	"AfterFunc": true,
	"NewTimer":  true,
	"NewTicker": true,
	"Sleep":     true,
}

// DefaultDenyImports returns a copy of the built-in import denylist.
func DefaultDenyImports() []string {
	return append([]string(nil), defaultDeny...)
}

// LoadPolicy reads a YAML policy file; a missing file yields the zero Policy.
func LoadPolicy(path string) (Policy, error) {
	var p Policy
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil && !errors.Is(err, io.EOF) {
		return Policy{}, fmt.Errorf("purity: parse %s: %w", path, err)
	}
	return p, nil
}

// allowsGlobal reports whether the package-level variable name in pkg is allowlisted.
func (p Policy) allowsGlobal(pkg, name string) bool {
	for _, g := range p.AllowGlobals {
		if g == name || g == pkg+"."+name {
			return true
		}
	}
	return false
}

// allowsCall reports whether the qualified call such as "time.Since" is allowlisted.
func (p Policy) allowsCall(qualified string) bool {
	for _, c := range p.AllowCalls {
		if c == qualified {
			return true
		}
	}
	return false
}
