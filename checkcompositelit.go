package eval

import (
	"reflect"
	"go/ast"
)

func checkCompositeLit(ctx *Ctx, lit *ast.CompositeLit, env *Env) (*CompositeLit, []error) {
	return checkCompositeLitR(ctx, lit, nil, env)
}

// Recursively check composite literals, where a child composite lit's type depends the
// parent's type For example, the expression [][]int{{1,2},{3,4}} contains two
// slice lits, {1,2} and {3,4}, but their types are inferenced from the parent [][]int{}.
func checkCompositeLitR(ctx *Ctx, lit *ast.CompositeLit, t reflect.Type, env *Env) (*CompositeLit, []error) {
	alit := &CompositeLit{CompositeLit: lit}

	// We won't generate any errors here if the given type does not match lit.Type.
	// The caller will need to detect the type incompatibility.
	if lit.Type != nil {
		var errs []error
		lit.Type, t, _, errs = checkType(ctx, lit.Type, env)
		if errs != nil {
			return alit, errs
		}
	} else if t == nil {
		return alit, []error{ErrMissingCompositeLitType{at(ctx, alit)}}
	}

	alit.knownType = knownType{t}

	switch t.Kind() {
	case reflect.Map:
		return checkCompositeLitMap(ctx, alit, t, env)
	case reflect.Array, reflect.Slice:
		return checkCompositeLitArrayOrSlice(ctx, alit, t, env)
	case reflect.Struct:
		return checkCompositeLitStruct(ctx, alit, t, env)
	default:
		panic("eval: unimplemented composite lit " + t.Kind().String())
	}
}

func checkCompositeLitMap(ctx *Ctx, lit *CompositeLit, t reflect.Type, env *Env) (*CompositeLit, []error) {
	var errs, moreErrs []error

	kT := t.Key()

	// Don't check for duplicate interface{} keys. This is a gc bug
	// http://code.google.com/p/go/issues/detail?id=7214
	var seen map[interface{}] bool
	if kT.Kind() != reflect.Interface {
		seen = make(map[interface{}] bool, len(lit.Elts))
	}
	eltT := t.Elem()

	for i := range lit.Elts {
		if kv, ok := lit.Elts[i].(*ast.KeyValueExpr); !ok {
			lit.Elts[i], moreErrs = CheckExpr(ctx, lit.Elts[i], env)
			if moreErrs != nil {
				errs = append(errs, moreErrs...)
			}
			errs = append(errs, ErrMissingMapKey{at(ctx, lit.Elts[i])})
		} else {
			lit.Elts[i] = &KeyValueExpr{KeyValueExpr: kv}
			k, ok, moreErrs := checkExprAssignableTo(ctx, kv.Key, kT, env)
			if !ok {
				if len(k.KnownType()) != 0 {
					kF := fakeCheckExpr(kv.Key, env)
					kF.setKnownType(knownType(k.KnownType()))
					errs = append(errs, ErrBadMapKey{at(ctx, kF), kT})
				}
			} else {
				errs = append(errs, moreErrs...)
			}
			kv.Key = k

			if seen != nil && k.IsConst() {
				var constKey interface{}
				if k.KnownType()[0] == ConstNil {
					constKey = nil
				} else if cT, ok := k.KnownType()[0].(ConstType); ok {
					c, _ := promoteConstToTyped(ctx, cT, constValue(k.Const()),
						cT.DefaultPromotion(), k)
					constKey = reflect.Value(c).Interface()
				} else {
					constKey = k.Const().Interface()
				}
				if seen[constKey] {
					errs = append(errs, ErrDuplicateMapKey{at(ctx, kv.Key)})
				}
				seen[constKey] = true
			}
			v, moreErrs := checkMapValue(ctx, kv.Value, eltT, env)
			if moreErrs != nil {
				errs = append(errs, moreErrs...)
			}
			kv.Value = v
		}
	}
	return lit, errs
}

func checkCompositeLitArrayOrSlice(ctx *Ctx, lit *CompositeLit, t reflect.Type, env *Env) (*CompositeLit, []error) {
	var errs, moreErrs []error
	eltT := t.Elem()
	maxIndex, curIndex := -1, 0
	outOfBounds := false
	length := -1
	if t.Kind() == reflect.Array {
		length = t.Len()
	}
	used := make(map[int] bool, len(lit.Elts))
	// Check all keys are valid and calculate array or slice length.
	// Elements with key are placed at the keyed position.
	// Elements without are placed in the next position.
	// For example, []int{1, 2:1, 1} -> [1, 0, 1, 1]
	for i := range lit.Elts {
		var value *ast.Expr
		kv, ok := lit.Elts[i].(*ast.KeyValueExpr)
		if !ok {
			value = &lit.Elts[i]
		} else {
			lit.Elts[i] = &KeyValueExpr{KeyValueExpr: kv}
			value = &kv.Value
			// Check the array key
			var index int
			kv.Key, index, ok, moreErrs = checkArrayIndex(ctx, kv.Key, env);
			if !ok || moreErrs != nil {
				// NOTE[crc] Haven't checked the gc implementation, but
				// from experimentation it seems that only undefined
				// idents are reported. This filter should perhaps be part
				// of checkArrayIndex
				for _, err := range moreErrs {
					if _, ok := err.(ErrUndefined); ok {
						errs = append(errs, err)
					}
				}
				errs = append(errs, ErrBadArrayKey{at(ctx, kv.Key)})
				// Don't include this element in index calculations
				curIndex -= 1
				goto check
			}
			lit.indices = append(lit.indices, struct{pos, index int}{i, index})
			curIndex = index
		}
		if maxIndex < curIndex {
			maxIndex = curIndex
		}
		if !outOfBounds && length != -1 && curIndex >= length {
			outOfBounds = true
			errs = append(errs, ErrArrayKeyOutOfBounds{at(ctx, lit.Elts[i]), t, curIndex})
		}
		// has this index been used already
		if used[curIndex] {
			errs = append(errs, ErrDuplicateArrayKey{at(ctx, kv.Key), curIndex})
		}
		used[curIndex] = true

check:
		// finally check the value
		*value, moreErrs = checkArrayValue(ctx, *value, eltT, env)
		if moreErrs != nil {
			errs = append(errs, moreErrs...)
		}

		curIndex += 1
	}
	lit.indices = append(lit.indices, struct{pos, index int}{-1, -1})
	if length == -1 {
		lit.length = maxIndex + 1
	} else {
		lit.length = length
	}
	return lit, errs
}

func checkCompositeLitStruct(ctx *Ctx, lit *CompositeLit, t reflect.Type, env *Env) (*CompositeLit, []error) {
	var errs, moreErrs []error

	// X{} is treated as if it has zero KeyValue'd elements, i.e. unspecified
	// elements are set to zero. This is always valid
	if len(lit.Elts) == 0 {
		return lit, nil
	}

	// gc first checks if there are ANY keys present, and then decides how
	// to process the initialisers.
	keysPresent := false
	for _, elt := range lit.Elts {
		_, ok := elt.(*ast.KeyValueExpr)
		keysPresent = keysPresent || ok
	}

	if keysPresent {
		seen := make(map[string] bool, len(lit.Elts))
		mixed := false
		for i := 0; i < len(lit.Elts); i += 1 {
			kv, ok := lit.Elts[i].(*ast.KeyValueExpr)
			if !ok {
				if !mixed {
					// This error only gets reported once
					mixed = true
					errs = append(errs, ErrMixedStructValues{at(ctx, lit.Elts[i])})
				}
				continue
			}

			lit.Elts[i] = &KeyValueExpr{KeyValueExpr: kv}
			// Check the key is a struct member
			if ident, ok := kv.Key.(*ast.Ident); !ok {
				// This check is a hack for making kv.Key printable.
				// field identifiers should not usually be type checked.
				kv.Key = fakeCheckExpr(kv.Key, env)
				errs = append(errs, ErrInvalidStructField{at(ctx, kv.Key)})
			} else if name := ident.Name; false {
			} else if field, ok := t.FieldByName(name); !ok {
				errs = append(errs, ErrUnknownStructField{at(ctx, kv.Key), t, name})
			} else {
				if seen[name] {
					errs = append(errs, ErrDuplicateStructField{at(ctx, kv.Key), name})
				}
				seen[name] = true
				lit.fields = append(lit.fields, field.Index[0])
				kv.Value, moreErrs = checkStructField(ctx, kv.Value, field, env)
				if moreErrs != nil {
					errs = append(errs, moreErrs...)
				}
			}
		}
	} else {
		numFields := t.NumField()
		var i int
		for i = 0; i < numFields && i < len(lit.Elts); i += 1 {
			field := t.Field(i)
			lit.Elts[i], moreErrs = checkStructField(ctx, lit.Elts[i], field, env)
			if moreErrs != nil {
				errs = append(errs, moreErrs...)
			}
			lit.fields = append(lit.fields, i)
		}
		if numFields != len(lit.Elts) {
			errs = append(errs, ErrWrongNumberOfStructValues{at(ctx, lit)})
		}
		// Remaining fields are type checked reguardless of use
		for ; i < len(lit.Elts); i += 1 {
			lit.Elts[i], moreErrs = CheckExpr(ctx, lit.Elts[i], env)
			if moreErrs != nil {
				errs = append(errs, moreErrs...)
			}
		}
	}
	return lit, errs
}

func checkMapValue(ctx *Ctx, expr ast.Expr, eltT reflect.Type, env *Env) (Expr, []error) {
	switch eltT.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.Struct:
		if lit, ok := expr.(*ast.CompositeLit); ok {
			return checkCompositeLitR(ctx, lit, eltT, env)
		}
	}

	aexpr, ok, errs := checkExprAssignableTo(ctx, expr, eltT, env)
	if !ok {
		// NOTE[crc] this hack removes conversion errors from consts other
		// than strings and nil to match the output of gc.
		if ccerr, ok := errs[0].(ErrBadConstConversion); ok {
			if ccerr.from == ConstNil {
				// No ErrBadMapValue for nil
				return aexpr, errs
			} else if ccerr.from != ConstString {
				// gc implementation only displays string conversion errors
				errs = nil
			}
		}
		errs = append(errs, ErrBadMapValue{at(ctx, aexpr), eltT})
	}
	return aexpr, errs
}

func checkArrayValue(ctx *Ctx, expr ast.Expr, eltT reflect.Type, env *Env) (Expr, []error) {
	switch eltT.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.Struct:
		if lit, ok := expr.(*ast.CompositeLit); ok {
			return checkCompositeLitR(ctx, lit, eltT, env)
		}
	}

	aexpr, ok, errs := checkExprAssignableTo(ctx, expr, eltT, env)
	if !ok {
		// NOTE[crc] this hack removes conversion errors from consts other
		// than strings and nil to match the output of gc.
		if ccerr, ok := errs[0].(ErrBadConstConversion); ok {
			if ccerr.from == ConstNil {
				// No ErrBadArrayValue for nil
				return aexpr, errs
			} else if ccerr.from != ConstString {
				// gc implementation only displays string conversion errors
				errs = nil
			}
		}
		errs = append(errs, ErrBadArrayValue{at(ctx, aexpr), eltT})
	}
	return aexpr, errs
}

func checkStructField(ctx *Ctx, expr ast.Expr, field reflect.StructField, env *Env) (Expr, []error) {
	aexpr, ok, errs := checkExprAssignableTo(ctx, expr, field.Type, env)
	if !ok {
		errs = append([]error{}, ErrBadStructValue{at(ctx, aexpr), field.Type})
	}
	return aexpr, errs
}
