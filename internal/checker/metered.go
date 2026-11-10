package checker

// Lazy Starlark values, like range(10**12) or text.codepoints(), can feed
// any number of items to code written in Go: list.extend(), f(*args) or
// dict(). The wrappers here count a step for every item, so that such a
// loop stops with the check.

import (
	"unicode/utf8"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// meteredIterable wraps a lazy iterable, like text.codepoints().
type meteredIterable struct {
	inner  starlark.Iterable
	thread *starlark.Thread
	// source is the text or bytes value of text.elems() and the like, or
	// nil. The text of the iterable holds it in full.
	source starlark.Value
	// length is the number of items, or -1 when it is not known. Size
	// checks use it; len() does not.
	length int
}

func (m meteredIterable) String() string        { return m.inner.String() }
func (m meteredIterable) Type() string          { return m.inner.Type() }
func (m meteredIterable) Freeze()               { m.inner.Freeze() }
func (m meteredIterable) Truth() starlark.Bool  { return m.inner.Truth() }
func (m meteredIterable) Hash() (uint32, error) { return m.inner.Hash() }

func (m meteredIterable) Iterate() starlark.Iterator {
	return &meteredIterator{inner: m.inner.Iterate(), meter: newMeter(m.thread)}
}

// meteredIndexable is meteredIterable for values that also have a length
// and items by index, like text.elems().
type meteredIndexable struct {
	meteredIterable
	indexable starlark.Indexable
}

func (m meteredIndexable) Len() int                   { return m.indexable.Len() }
func (m meteredIndexable) Index(i int) starlark.Value { return m.indexable.Index(i) }

// meteredRange is Starlark's range with a metered loop. Length, index,
// slices, "in" and "==" work as before.
type meteredRange struct {
	meteredIndexable
	r starlark.Value
}

func newMeteredRange(r starlark.Value, thread *starlark.Thread) meteredRange {
	indexable := r.(starlark.Indexable)
	return meteredRange{
		meteredIndexable: meteredIndexable{
			meteredIterable: meteredIterable{inner: r.(starlark.Iterable), thread: thread, length: indexable.Len()},
			indexable:       indexable,
		},
		r: r,
	}
}

func (m meteredRange) Slice(start, end, step int) starlark.Value {
	return newMeteredRange(m.r.(starlark.Sliceable).Slice(start, end, step), m.thread)
}

func (m meteredRange) Has(y starlark.Value) (bool, error) {
	return m.r.(starlark.Container).Has(y)
}

func (m meteredRange) CompareSameType(op syntax.Token, y starlark.Value, depth int) (bool, error) {
	return starlark.CompareDepth(op, m.r, y.(meteredRange).r, depth)
}

// meteredIterator counts a step for every item and ends early when the
// check must stop. The check then fails at its next step; Go code that
// goes through the items must check for the stop after the loop (see
// iterate).
type meteredIterator struct {
	inner starlark.Iterator
	meter meter
}

func (it *meteredIterator) Next(p *starlark.Value) bool {
	if it.meter.step() != nil {
		return false
	}
	return it.inner.Next(p)
}

func (it *meteredIterator) Done() { it.inner.Done() }

// rangeBuiltin is range(), with a metered loop.
func rangeBuiltin(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	r, err := starlark.Universe["range"].(*starlark.Builtin).CallInternal(thread, args, kwargs)
	if err != nil {
		return nil, err
	}
	return newMeteredRange(r, thread), nil
}

// meteredMethod calls the Starlark text or bytes method of the same name,
// like elems() or codepoints(), and meters the result.
func meteredMethod(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	receiver := b.Receiver()
	native, err := receiver.(starlark.HasAttrs).Attr(b.Name())
	if err != nil {
		return nil, err
	}
	v, err := starlark.Call(thread, native, args, kwargs)
	if err != nil {
		return nil, err
	}
	iterable, ok := v.(starlark.Iterable)
	if !ok {
		return v, nil
	}
	metered := meteredIterable{inner: iterable, thread: thread, source: receiver, length: -1}
	if indexable, ok := v.(starlark.Indexable); ok {
		metered.length = indexable.Len()
		return meteredIndexable{meteredIterable: metered, indexable: indexable}, nil
	}
	// The rest give one item per letter (text.codepoints()) or per byte
	// (bytes.elems()).
	switch r := receiver.(type) {
	case starlark.String:
		metered.length = utf8.RuneCountInString(string(r))
	case starlark.Bytes:
		metered.length = len(r)
	}
	return metered, nil
}

// sourceSize returns the size of the text or bytes value that the text of
// v holds in full, like the text of text.elems(), or 0.
func sourceSize(v starlark.Value) int {
	var source starlark.Value
	switch v := v.(type) {
	case meteredIterable:
		source = v.source
	case meteredIndexable:
		source = v.source
	}
	switch s := source.(type) {
	case starlark.String:
		return len(s)
	case starlark.Bytes:
		return len(s)
	}
	return 0
}
