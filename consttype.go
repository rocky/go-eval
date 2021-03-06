package eval

import "reflect"

// Type ConstType can annotate information needed for evaluating const
// expressions. It should not be used with the reflect package.
type ConstType interface {
	reflect.Type
	IsIntegral() bool
	IsReal() bool
	IsNumeric() bool

	// Format for "something bad X (type X.ErrorType())"
	ErrorType() string

	// The go type the ConstType is promoted to by default
	DefaultPromotion() reflect.Type
}

type ConstIntType struct { reflect.Type }
type ConstRuneType struct { reflect.Type }
type ConstFloatType struct { reflect.Type }
type ConstComplexType struct { reflect.Type }
type ConstStringType struct { reflect.Type }
type ConstNilType struct { reflect.Type }
type ConstBoolType struct { reflect.Type }

var (
	ConstInt = ConstIntType { reflect.TypeOf(0) }
	ConstRune = ConstRuneType { reflect.TypeOf('\000') }
	ConstFloat = ConstFloatType { reflect.TypeOf(0.0) }
	ConstComplex = ConstComplexType { reflect.TypeOf(0i) }
	ConstString = ConstStringType { reflect.TypeOf("") }
	ConstNil = ConstNilType { nil }
	ConstBool = ConstBoolType { reflect.TypeOf(false) }
)

// These are actually the names of the default const promotions
func (ConstIntType) String() string { return "int" }
func (ConstRuneType) String() string { return "rune" }
func (ConstFloatType) String() string { return "float64" }
func (ConstComplexType) String() string { return "complex128" }
func (ConstStringType) String() string { return "string" }
func (ConstNilType) String() string { return "<T>" }
func (ConstBoolType) String() string { return "bool" }

func (ConstIntType) ErrorType() string { return "untyped number" }
func (ConstRuneType) ErrorType() string { return "untyped number" }
func (ConstFloatType) ErrorType() string { return "untyped number" }
func (ConstComplexType) ErrorType() string { return "untyped number" }
func (ConstStringType) ErrorType() string { return "untyped string" }
func (ConstNilType) ErrorType() string { return "nil" }
func (ConstBoolType) ErrorType() string { return "untyped bool" }

func (c ConstIntType) DefaultPromotion() reflect.Type { return c.Type }
func (c ConstRuneType) DefaultPromotion() reflect.Type { return c.Type }
func (c ConstFloatType) DefaultPromotion() reflect.Type { return c.Type }
func (c ConstComplexType) DefaultPromotion() reflect.Type { return c.Type }
func (c ConstStringType) DefaultPromotion() reflect.Type { return c.Type }
func (c ConstNilType) DefaultPromotion() reflect.Type { return c.Type }
func (c ConstBoolType) DefaultPromotion() reflect.Type { return c.Type }

func (ConstIntType) IsIntegral() bool { return true }
func (ConstRuneType) IsIntegral() bool { return true }
func (ConstFloatType) IsIntegral() bool { return false }
func (ConstComplexType) IsIntegral() bool { return false }
func (ConstStringType) IsIntegral() bool { return false }
func (ConstNilType) IsIntegral() bool { return false }
func (ConstBoolType) IsIntegral() bool { return false }

func (ConstIntType) IsReal() bool { return true }
func (ConstRuneType) IsReal() bool { return true }
func (ConstFloatType) IsReal() bool { return true }
func (ConstComplexType) IsReal() bool { return false }
func (ConstStringType) IsReal() bool { return false }
func (ConstNilType) IsReal() bool { return false }
func (ConstBoolType) IsReal() bool { return false }

func (ConstIntType) IsNumeric() bool { return true }
func (ConstRuneType) IsNumeric() bool { return true }
func (ConstFloatType) IsNumeric() bool { return true }
func (ConstComplexType) IsNumeric() bool { return true }
func (ConstStringType) IsNumeric() bool { return false }
func (ConstNilType) IsNumeric() bool { return false }
func (ConstBoolType) IsNumeric() bool { return false }

// promoteConsts returns the ConstType of a binary, a non-boolean,
// expression involving const types of x and y.  Errors match those
// produced by gc and are as follows:
func promoteConsts(ctx *Ctx, x, y ConstType, xexpr, yexpr Expr, xval, yval reflect.Value) (ConstType, []error) {
	switch x.(type) {
	case ConstIntType, ConstRuneType, ConstFloatType, ConstComplexType:
		switch y.(type) {
		case ConstIntType, ConstRuneType, ConstFloatType, ConstComplexType:
			return promoteConstNumbers(x, y), nil
		}
		return nil, []error{ErrBadConstConversion{at(ctx, yexpr), y, x, yval}}
	case ConstStringType:
		switch y.(type) {
		case ConstStringType:
			return x, nil
		case ConstIntType, ConstRuneType, ConstFloatType, ConstComplexType:
			return nil, []error{ErrBadConstConversion{at(ctx, xexpr), x, y, xval}}
		default:
			return nil, []error{
				ErrBadConstConversion{at(ctx, xexpr), x, ConstInt, xval},
				ErrBadConstConversion{at(ctx, yexpr), y, ConstInt, yval},
			}
		}
	case ConstNilType:
		switch y.(type) {
		case ConstNilType:
			return x, nil
		case ConstIntType, ConstRuneType, ConstFloatType, ConstComplexType:
			return nil, []error{ErrBadConstConversion{at(ctx, xexpr), x, y, xval}}
		default:
			return nil, []error{
				ErrBadConstConversion{at(ctx, xexpr), x, ConstInt, xval},
				ErrBadConstConversion{at(ctx, yexpr), y, ConstInt, yval},
			}
		}
	case ConstBoolType:
		switch y.(type) {
		case ConstBoolType:
			return x, nil
		case ConstIntType, ConstRuneType, ConstFloatType, ConstComplexType, ConstStringType, ConstNilType:
			return nil, []error{ErrBadConstConversion{at(ctx, yexpr), y, x, yval}}
		}
	}
	panic("go-interactive: impossible")
}

// promoteConstNumbers can't fail, but panics if x or y are not
// Const(Int|Rune|Float|Complex)Type
func promoteConstNumbers(x, y ConstType) ConstType {
	switch x.(type) {
	case ConstIntType:
		switch y.(type) {
		case ConstIntType:
			return x
		case ConstRuneType, ConstFloatType, ConstComplexType:
			return y
		}
	case ConstRuneType:
		switch y.(type) {
		case ConstIntType, ConstRuneType:
			return x
		case ConstFloatType, ConstComplexType:
			return y
		}
	case ConstFloatType:
		switch y.(type) {
		case ConstIntType, ConstRuneType, ConstFloatType:
			return x
		case ConstComplexType:
			return y
		}
	case ConstComplexType:
		switch y.(type) {
		case ConstIntType, ConstRuneType, ConstFloatType, ConstComplexType:
			return x
		}
	}
	panic("go-interactive: promoteConstNumbers called with non-numbers")
}

// Convert an untyped constant to a typed constant, where it would be
// legal to do using a type cast.
func castConstToTyped(ctx *Ctx, from ConstType, c constValue, to reflect.Type, expr Expr) (
	constValue, []error) {
        return convertConstToTyped(ctx, from, c, to, true, expr)
}

// Convert an untyped constant to a typed constant, where it would be
// legal to do so automatically in a binary expression.
func promoteConstToTyped(ctx *Ctx, from ConstType, c constValue, to reflect.Type, expr Expr) (
	constValue, []error) {
        return convertConstToTyped(ctx, from, c, to, false, expr)
}

// Convert an untyped constant to a typed constant. If the types from and to are
// incompatible, ErrBadConstConversion is returned along with an invalid value.
// If the types were compatible but other errors are present, such as integer
// overflows or floating truncations, the conversion will continue and a valid
// value will be returned. Therefore, if a valid value is returned, the const
// type is assignable to the reflect.Type. This can be checked using
//
//  reflect.Value(constValue).IsValid()
//
func convertConstToTyped(ctx *Ctx, from ConstType, c constValue, to reflect.Type, isTypeCast bool, expr Expr) (
	constValue, []error) {
	v := hackedNew(to).Elem()

	switch from.(type) {
	case ConstIntType, ConstRuneType, ConstFloatType, ConstComplexType:
		underlying := reflect.Value(c).Interface().(*ConstNumber)
		switch to.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			var errs []error
			i, truncation, overflow := underlying.Value.Int(to.Bits())
			if truncation {
				errs = append(errs, ErrTruncatedConstant{at(ctx, expr), ConstInt, underlying})
			}
			if overflow {
				errs = append(errs, ErrOverflowedConstant{at(ctx, expr), from, to, underlying})
			}
			// For some reason, the errors produced are "complex -> int" then "complex -> real"
			_, truncation = underlying.Value.Real()
			if truncation {
				errs = append(errs, ErrTruncatedConstant{at(ctx, expr), ConstFloat, underlying})
			}
			v.SetInt(i)
			return constValue(v), errs

		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			var errs []error
			u, truncation, overflow := underlying.Value.Uint(to.Bits())
			if truncation {
				errs = append(errs, ErrTruncatedConstant{at(ctx, expr), ConstInt, underlying})
			}
			if overflow {
				errs = append(errs, ErrOverflowedConstant{at(ctx, expr), from, to, underlying})
			}
			// For some reason, the erros produced are "complex -> int" then "complex -> real"
			_, truncation = underlying.Value.Real()
			if truncation {
				errs = append(errs, ErrTruncatedConstant{at(ctx, expr), ConstFloat, underlying})
			}
			v.SetUint(u)
			return constValue(v), errs

		case reflect.Float32, reflect.Float64:
			var errs []error
			f, truncation, _ := underlying.Value.Float64()
			if truncation {
				errs = []error{ErrTruncatedConstant{at(ctx, expr), ConstFloat, underlying}}
			}
			v.SetFloat(f)
			return constValue(v), errs

		case reflect.Complex64, reflect.Complex128:
			cmplx, _ := underlying.Value.Complex128()
			v.SetComplex(cmplx)
			return constValue(v), nil

		// string(97) is legal, equivalent of string('a'), but this
                // conversion is not automatic. "abc" + 10 is illegal.
		case reflect.String:
			if isTypeCast && from.IsIntegral() {
				i, _, overflow := underlying.Value.Int(32)
				if overflow {
					err := ErrOverflowedConstant{at(ctx, expr), from, ConstString, underlying}
					return constValue{}, []error{err}
				}
				v.SetString(string(i))
				return constValue(v), nil
			}

		// consts can satisfy the empty interface only
		case reflect.Interface:
			if to == emptyInterface {
				to = underlying.Type.DefaultPromotion()
				cv, _ := convertConstToTyped(ctx, from, c, to, isTypeCast, expr)
				v.Set(reflect.Value(cv).Convert(emptyInterface))
				return constValue(v), nil
			}
		}

	case ConstStringType:
		if to.Kind() == reflect.String {
			v.SetString(reflect.Value(c).String())
			return constValue(v), nil
		} else if to == emptyInterface {
			v.Set(reflect.Value(c).Convert(emptyInterface))
			return constValue(v), nil
		} else if isTypeCast && to == byteSlice {
			v = reflect.Value(c).Convert(to)
			return constValue(v), nil
		}

	case ConstBoolType:
		if to.Kind() == reflect.Bool {
			v.SetBool(reflect.Value(c).Bool())
			return constValue(v), nil
		} else if to == emptyInterface {
			v.Set(reflect.Value(c).Convert(emptyInterface))
			return constValue(v), nil
		}

	case ConstNilType:
		// Unfortunately there is no reflect.Type.CanNil()
		if isNillable(to) {
			// v is already nil
			return constValue(v), nil
		}
	}

	return constValue{}, []error{ErrBadConstConversion{at(ctx, expr), from, to, reflect.Value(c)}}
}

// Convert a typed numeric value to a const number. Ok is false if v is not numeric
// Because overflowing constants would result in loss of precision in error messages,
// the Expr.Const() method of nodes containing such errors return a *ConstNumber
// instead of the typed value. Because of this, if v is a *ConstNumber, the
// underlying const number will be returned. When working with a successfully type
// checked tree a typed node's Expr.Const() method will never return a *ConstNumber.
func convertTypedToConstNumber(v reflect.Value) (_ *ConstNumber, ok bool) {
	switch v.Type().Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return NewConstInt64(v.Int()), true

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return NewConstUint64(v.Uint()), true

	case reflect.Float32, reflect.Float64:
		return NewConstFloat64(v.Float()), true

	case reflect.Complex64, reflect.Complex128:
		return NewConstComplex128(v.Complex()), true

	default:
                if n, ok := v.Interface().(*ConstNumber); ok {
                        return n, true
                } else {
		        return nil, false
                }
	}
}

