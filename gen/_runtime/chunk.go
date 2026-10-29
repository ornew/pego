package runtime

// Chunk owns unused values and the next allocation size for one type. Generated parsers keep one
// per type of a typed value and take the values from it (Alloc, AllocSlice). Handed-out values
// are never reused, even when a parse discards them.
//
// Chunk is for generated parsers; not a stable API.
type Chunk[T any] struct {
	free      []T
	next      int
	bootstrap uint8
}

// grow starts small and doubles up to limit. The request is always satisfied.
func (s *Chunk[T]) grow(n, initial, limit int) {
	size := s.next
	if size == 0 {
		size = initial
	}
	for size < n {
		size *= 2
	}
	s.free = make([]T, size)
	s.next = size * 2
	if s.next > limit {
		s.next = limit
	}
}

// Alloc returns a new zero value from the chunk, which grows from 8 to 256 values. It allocates the
// first three values independently, so a later published value cannot retain discarded early
// siblings through a chunk.
//
// Alloc is for generated parsers; not a stable API.
func Alloc[T any](s *Chunk[T]) *T {
	if len(s.free) == 0 {
		if s.bootstrap < 3 {
			s.bootstrap++
			return new(T)
		}
		s.grow(1, 8, 256)
	}
	v := &s.free[0]
	s.free = s.free[1:]
	return v
}

// AllocSlice returns n zero values with capacity n, so appending cannot overwrite another returned
// slice. Small-list chunks grow from 64 to 1,024 elements; a list of more than 256 elements is
// allocated on its own, and so is the first nonempty small list, to keep its backing values
// separate from later published lists of the same type. An empty list is not nil.
//
// AllocSlice is for generated parsers; not a stable API.
func AllocSlice[T any](s *Chunk[T], n int) []T {
	if n == 0 {
		return []T{} // an empty list, not nil
	}
	if n > 256 {
		return make([]T, n)
	}
	if len(s.free) < n {
		if s.bootstrap == 0 {
			s.bootstrap = 1
			return make([]T, n)
		}
		s.grow(n, 64, 1024)
	}
	v := s.free[:n:n]
	s.free = s.free[n:]
	return v
}
