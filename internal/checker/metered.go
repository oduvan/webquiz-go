package checker

// Lazy Starlark values, like range(10**12) or text.codepoints(), can feed
// any number of items to code written in Go: list.extend(), f(*args) or
// dict(). The wrappers here count a step for every item, so that such a
// loop stops with the check.

import (
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// meteredIterable wraps a lazy iterable, like text.codepoints().
type meteredIterable struct {
	inner  starlark.Iterable
	thread *starlark.Thread
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
	return meteredRange{
		meteredIndexable: meteredIndexable{
			meteredIterable: meteredIterable{inner: r.(starlark.Iterable), thread: thread},
			indexable:       r.(starlark.Indexable),
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
// check must stop. The check then fails at its next step.
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

// meteredMethod calls the Starlark text method of the same name, like
// elems() or codepoints(), and meters the result.
func meteredMethod(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	native, err := b.Receiver().(starlark.String).Attr(b.Name())
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
	metered := meteredIterable{inner: iterable, thread: thread}
	if indexable, ok := v.(starlark.Indexable); ok {
		return meteredIndexable{meteredIterable: metered, indexable: indexable}, nil
	}
	return metered, nil
}
