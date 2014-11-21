package debugtools

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// Derived from reflect.DeepEqual

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Deep equality test via reflection

// During deepValueEqual, must keep track of checks that are
// in progress.  The comparison algorithm assumes that all
// checks in progress are true when it reencounters them.
// Visited comparisons are stored in a map indexed by visit.
type visit struct {
	a1  uintptr
	a2  uintptr
	typ reflect.Type
}

type deepEqualState struct {
	visited map[visit]bool
	depth   int
	sub     bool
	w       io.Writer
}

func (s *deepEqualState) println(vals ...interface{}) {
	if s.sub {
		s.sub = false
	} else if s.depth > 0 {
		fmt.Fprint(s.w, strings.Repeat("  ", s.depth))
	}
	fmt.Fprintln(s.w, vals...)
}

func (s *deepEqualState) printf(format string, vals ...interface{}) {
	if s.sub {
		s.sub = false
	} else if s.depth > 0 {
		fmt.Fprint(s.w, strings.Repeat("  ", s.depth))
	}
	fmt.Fprintf(s.w, format, vals...)
}

func (s *deepEqualState) incDepth() {
	s.depth++
}

func (s *deepEqualState) decDepth() {
	s.depth--
}

// Tests for deep equality using reflected types. The map argument tracks
// comparisons that have already been seen, which allows short circuiting on
// recursive types.
func (s *deepEqualState) deepValueEqual(v1, v2 reflect.Value) bool {
	s.incDepth()
	defer s.decDepth()

	if !v1.IsValid() || !v2.IsValid() {
		s.println("Something is not valid:", v1, v2)
		return v1.IsValid() == v2.IsValid()
	}
	if v1.Type() != v2.Type() {
		s.println("Types don't match")
		return false
	}

	// if depth > 10 { panic("deepValueEqual") }	// for debugging
	hard := func(k reflect.Kind) bool {
		switch k {
		case reflect.Array, reflect.Map, reflect.Slice, reflect.Struct:
			return true
		}
		return false
	}

	if v1.CanAddr() && v2.CanAddr() && hard(v1.Kind()) {
		addr1 := v1.UnsafeAddr()
		addr2 := v2.UnsafeAddr()
		if addr1 > addr2 {
			// Canonicalize order to reduce number of entries in visited.
			addr1, addr2 = addr2, addr1
		}

		// Short circuit if references are identical ...
		if addr1 == addr2 {
			s.println("  Same address, so equal")
			return true
		}

		// ... or already seen
		typ := v1.Type()
		v := visit{addr1, addr2, typ}
		if s.visited[v] {
			s.println("  Already visited, so equal")
			return true
		}

		// Remember for later.
		s.visited[v] = true
	}

	switch v1.Kind() {
	case reflect.Array:
		s.println("Comparing arrays of type:", v1.Type())
		for i := 0; i < v1.Len(); i++ {
			if !s.deepValueEqual(v1.Index(i), v2.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Slice:
		s.println("Comparing slices of type:", v1.Type())
		if v1.IsNil() != v2.IsNil() {
			s.printf("  %#v != %#v\n", v1.Interface(), v2.Interface())
			s.println("  One of the slices is nil, so not equal")
			return false
		}
		if v1.Len() != v2.Len() {
			s.println("  Unequal lengths, so not equal")
			return false
		}
		if v1.Pointer() == v2.Pointer() {
			s.println("  Pointers equal, so equal")
			return true
		}
		for i := 0; i < v1.Len(); i++ {
			if !s.deepValueEqual(v1.Index(i), v2.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Interface:
		s.println("Comparing interfaces of type:", v1.Type())
		if v1.IsNil() || v2.IsNil() {
			s.println("  One of the interfaces is nil, so not equal")
			return v1.IsNil() == v2.IsNil()
		}
		return s.deepValueEqual(v1.Elem(), v2.Elem())
	case reflect.Ptr:
		s.println("Comparing pointers of type:", v1.Type())
		return s.deepValueEqual(v1.Elem(), v2.Elem())
	case reflect.Struct:
		s.println("Comparing structs of type:", v1.Type())
		for i, n := 0, v1.NumField(); i < n; i++ {
			s.printf("  %v: ", v1.Type().Field(i).Name)
			s.sub = true
			if !s.deepValueEqual(v1.Field(i), v2.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		s.println("Comparing map of type:", v1.Type())
		if v1.IsNil() != v2.IsNil() {
			s.println("  One of the maps is nil, so not equal")
			return false
		}
		if v1.Len() != v2.Len() {
			s.println("  Lengths don't match, so not equal")
			return false
		}
		if v1.Pointer() == v2.Pointer() {
			s.println("  Same pointer, so equal")
			return true
		}
		for _, k := range v1.MapKeys() {
			s.printf("%#v: ", k)
			s.sub = true
			if !s.deepValueEqual(v1.MapIndex(k), v2.MapIndex(k)) {
				return false
			}
		}
		return true
	case reflect.Func:
		if v1.IsNil() && v2.IsNil() {
			s.println("  Both nil functions, so equal")
			return true
		}
		// Can't do better than this:
		s.println("  Not both nil functions, so not equal")
		return false
	default:
		// Normal equality suffices
		if eq := reflect.DeepEqual(v1.Interface(), v2.Interface()); eq {
			s.printf("%#v == %#v\n", v1.Interface(), v2.Interface())
			return true
		} else {
			s.printf("%#v != %#v\n", v1.Interface(), v2.Interface())
			return false
		}
	}
}

// DeepEqual tests for deep equality. It uses normal == equality where
// possible but will scan elements of arrays, slices, maps, and fields of
// structs. In maps, keys are compared with == but elements use deep
// equality. DeepEqual correctly handles recursive types. Functions are equal
// only if they are both nil.
// An empty slice is not equal to a nil slice.
func DeepEqual(a1, a2 interface{}) (bool, string) {
	if a1 == nil || a2 == nil {
		return a1 == a2, ""
	}
	v1 := reflect.ValueOf(a1)
	v2 := reflect.ValueOf(a2)
	if v1.Type() != v2.Type() {
		return false, ""
	}
	buf := &bytes.Buffer{}
	s := &deepEqualState{
		visited: make(map[visit]bool),
		depth:   -1,
		sub:     false,
		w:       buf,
	}
	return s.deepValueEqual(v1, v2), string(buf.Bytes())
}
