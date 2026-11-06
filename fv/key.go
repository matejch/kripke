package fv

import (
	"bytes"
	"crypto/sha256"
	"reflect"

	"github.com/zeebo/xxh3"
)

type stateKey [16]byte

type keyer struct {
	enc       encoder
	renamings []map[reflect.Type]*symMap
	sha       bool
	best      []byte
}

func newKeyer(m *Model, o Options) *keyer {
	k := &keyer{sha: o.SHA256}
	if !o.NoSymmetry && len(m.Symmetries) > 0 {
		k.renamings = symRenamings(m.Symmetries)
	}
	return k
}

func (k *keyer) hash(b []byte) stateKey {
	if k.sha {
		sum := sha256.Sum256(b)
		var out stateKey
		copy(out[:], sum[:16])
		return out
	}
	return xxh3.Hash128(b).Bytes()
}

func (k *keyer) raw(s State) stateKey {
	k.enc.syms = nil
	return k.hash(encodeValue(&k.enc, s))
}

func (k *keyer) canonical(s State) stateKey {
	if len(k.renamings) == 0 {
		return k.raw(s)
	}
	k.best = k.best[:0]
	for i, r := range k.renamings {
		k.enc.syms = r
		b := encodeValue(&k.enc, s)
		if i == 0 || bytes.Compare(b, k.best) < 0 {
			k.best = append(k.best[:0], b...)
		}
	}
	k.enc.syms = nil
	return k.hash(k.best)
}

// Key returns the canonical encoding of s, without symmetry reduction.
func Key(s State) []byte {
	var e encoder
	return append([]byte(nil), encodeValue(&e, s)...)
}
