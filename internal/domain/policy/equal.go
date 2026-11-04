package policy

import "reflect"

// Equal reports whether two policies say the same thing. A nil list or map and an empty one are the same,
// because the file cannot tell them apart; any other difference, including the order of a list, is a change.
func Equal(a, b Policy) bool {
	return equalValue(reflect.ValueOf(a), reflect.ValueOf(b))
}

// equalValue walks structs field by field and collections element by element. Any other kind is a scalar and
// is compared with Value.Equal. Every field of a Policy is exported and no field is a pointer, so the walk needs
// no more cases than these.
func equalValue(a, b reflect.Value) bool {
	if a.Kind() == reflect.Struct {
		return equalFields(a, b)
	}
	if a.Kind() == reflect.Slice {
		return equalElements(a, b)
	}
	if a.Kind() == reflect.Map {
		return equalEntries(a, b)
	}
	return a.Equal(b)
}

func equalFields(a, b reflect.Value) bool {
	for i := range a.NumField() {
		if !equalValue(a.Field(i), b.Field(i)) {
			return false
		}
	}
	return true
}

func equalElements(a, b reflect.Value) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := range a.Len() {
		if !equalValue(a.Index(i), b.Index(i)) {
			return false
		}
	}
	return true
}

func equalEntries(a, b reflect.Value) bool {
	if a.Len() != b.Len() {
		return false
	}
	for _, k := range a.MapKeys() {
		bv := b.MapIndex(k)
		if !bv.IsValid() || !equalValue(a.MapIndex(k), bv) {
			return false
		}
	}
	return true
}
