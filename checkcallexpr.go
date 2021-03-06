package eval

import (
	"reflect"
	"go/ast"
	"go/token"
)

func checkCallExpr(ctx *Ctx, callExpr *ast.CallExpr, env *Env) (acall *CallExpr, errs []error) {
	acall = &CallExpr{CallExpr: callExpr}

	// First check for builtin calls. For new and make, the first argument is
	// a type, not a value. Therefore, allow the builtin checks to recursively
	// check their arguments
	if call, errs, isBuiltin := checkCallBuiltinExpr(ctx, acall, env); isBuiltin {
		return call, errs
	}

	// Recursively check arguments
	var moreErrs []error
	for i := range callExpr.Args {
		if acall.Args[i], moreErrs = CheckExpr(ctx, callExpr.Args[i], env); moreErrs != nil {
			errs = append(errs, moreErrs...)
		}
	}

	// First check if this expression is a type cast
	// Otherwise, assume a function call
	if typ, to, isType, moreErrs := checkType(ctx, acall.Fun, env); isType {
		if moreErrs != nil {
			return acall, append(errs, moreErrs...)
		}
		acall.Fun = typ
		return checkCallTypeExpr(ctx, acall, to, env)
	} else {
		return checkCallFunExpr(ctx, acall, env)
	}
}

func checkCallTypeExpr(ctx *Ctx, call *CallExpr, to reflect.Type, env *Env) (acall *CallExpr, errs []error) {
	call.knownType = []reflect.Type{to}
	call.isTypeConversion = true

	if len(call.Args) != 1 {
		return call, []error{ErrWrongNumberOfArgs{at(ctx, call), len(call.Args)}}
	}

	arg := call.Args[0].(Expr)
	from, err := expectSingleType(ctx, arg.KnownType(), arg)
	if err != nil {
		return call, []error{err}
	}

	if ct, ok := from.(ConstType); ok {
		// For bad constant conversions, gc produces two error
		// messages. E.g. string to uint64 cannot convert "abc"
		// to type uint64 cannot convert "abc" (type string) to
		// type uint64
		//
		// I've separated these into ErrBadConstConversiond and
		// ErrBadConversion The exception is if the conversion
		// is from nil
		v, errs := castConstToTyped(ctx, ct, constValue(arg.Const()), to, arg)
		if ct != ConstNil {
			if errs != nil {
				if b, ok := errs[0].(ErrBadConstConversion); ok {
					err := ErrBadConversion{b.ErrorContext, b.from, b.to, b.v}
					errs = append(errs, err)
				}
				// Some expr nodes will continue to generate
				// errors even if their children produce
				// errors. constValue must be set for this to
				// happen.
				call.constValue = constValue(arg.Const())
			} else {
				call.constValue = v
			}
		}
		return call, errs
	} else {
		if from.ConvertibleTo(to) {
			if arg.IsConst() {
				call.constValue = constValue(arg.Const().Convert(to))
			}
			return call, nil
		} else {
			return call, []error{ErrBadConstConversion{at(ctx, call), from, to, reflect.Value{}}}
		}
	}
}

func checkCallFunExpr(ctx *Ctx, call *CallExpr, env *Env) (*CallExpr, []error) {
	fun, errs := CheckExpr(ctx, call.Fun, env)
	if errs != nil && !fun.IsConst() {
		return call, errs
	}
	call.Fun = fun

	ftype, err := expectSingleType(ctx, fun.KnownType(), fun)
	if err != nil {
		return call, append(errs, err)
	// catch nil casts, e.g. nil(1)
	} else if ftype == ConstNil {
		return call, []error{ErrUntypedNil{at(ctx, fun)}}
	} else if ftype.Kind() != reflect.Func {
		return call, []error{ErrCallNonFuncType{at(ctx, fun)}}
	}

	call.knownType = make([]reflect.Type, ftype.NumOut())
	for i := range call.knownType {
		call.knownType[i] = ftype.Out(i)
	}

	// Some handly values
	variadic := ftype.IsVariadic()
	numIn := ftype.NumIn()

	// Special case handling doesn't play well with nil Args. Handle zero arg
	// functions first.
	if call.Args == nil {
		if numIn == 0 || (variadic && numIn == 1) {
			return call, nil
		} else {
			return call, []error{ErrWrongNumberOfArgs{at(ctx, call), len(call.Args)}}
		}
	}

	// Special case for f(g()), where g may return multiple values
	// The only way to verify that the multi-valued type of Args[0] arose
	// from function call is to dig through any ParenExpr and see if at
	// the bottom is another CallExpr
	arg0MultiValued := false
	arg0T := call.Args[0].(Expr).KnownType()
	if len(call.Args) == 1 && len(arg0T) > 1 {
		arg0 := call.Args[0].(Expr)
		arg0 = skipSuperfluousParens(arg0)
		if _, ok := arg0.(*CallExpr); ok {
			arg0MultiValued = true
		}
	}


	call.arg0MultiValued = arg0MultiValued
	if arg0MultiValued {
		// Check all but the last arg which will be handled specially
		var i int
		for i = 0; i < len(arg0T) && i < numIn-1; i += 1 {
			if !typeAssignableTo(arg0T[i], ftype.In(i)) {
				errs = append(errs, ErrWrongArgType{at(ctx, call.Args[0]), call, i})
			}
		}

		var argNT reflect.Type
		// Detect wrong number of args
		if !variadic {
			if len(arg0T) != numIn {
				return call, append(errs, ErrWrongNumberOfArgs{at(ctx, call), len(arg0T)})
			}
			argNT = ftype.In(i)
		} else {
			if len(arg0T) < numIn - 1 {
				return call, append(errs, ErrWrongNumberOfArgs{at(ctx, call), len(arg0T)})
			}
			argNT = ftype.In(i).Elem()
		}

		// Check remaining args
		for ; i < len(arg0T); i += 1 {
			if !typeAssignableTo(arg0T[i], argNT) {
				errs = append(errs, ErrWrongArgType{at(ctx, call.Args[0]), call, i})
			}
		}
	} else {
		argNEllipsis := call.Ellipsis != token.NoPos
		call.argNEllipsis = argNEllipsis

		// To match errors generated by gc, first check that all arguments are single
		// values. Proceed with type checking. In both cases, ErrWrongNumberOfArgs
		// must be considered last.
		skipTypeCheck := make([]bool, len(call.Args))
		for i, arg := range call.Args {
			expr := arg.(Expr)
			if _, err := expectSingleType(ctx, expr.KnownType(), expr); err != nil {
				errs = append(errs, err)
				skipTypeCheck[i] = true
			}
		}

		// Check all but the last arg which will be handled specially
		var i int
		for i = 0; i < len(call.Args) && i < numIn-1; i += 1 {
			if skipTypeCheck[i] {
				continue
			}
			expr := call.Args[i].(Expr)
			if ok, convErrs := exprAssignableTo(ctx, expr, ftype.In(i)); ok {
				errs = append(errs, convErrs...)
			} else {
				errs = append(errs, ErrWrongArgType{at(ctx, expr), call, i})
			}
		}

		var argNT reflect.Type
		if !variadic || argNEllipsis {
			if len(call.Args) != numIn {
				return call, append(errs, ErrWrongNumberOfArgs{at(ctx, call), len(call.Args)})
			}
			argNT = ftype.In(numIn - 1)
		} else {
			if len(call.Args) < numIn - 1 {
				return call, append(errs, ErrWrongNumberOfArgs{at(ctx, call), len(call.Args)})
			} else if len(call.Args) == numIn - 1 {
				// Variadic function with no ... args
				return call, errs
			}
			argNT = ftype.In(numIn - 1).Elem()
		}

		// Check remaining args
		for ; i < len(call.Args); i += 1 {
			if skipTypeCheck[i] {
				continue
			}
			expr := call.Args[i].(Expr)
			if ok, convErrs := exprAssignableTo(ctx, expr, argNT); ok {
				errs = append(errs, convErrs...)
			} else {
				errs = append(errs, ErrWrongArgType{at(ctx, expr), call, i})
			}
		}

		// Finally, check for illegal use of the ellipsis
		if !variadic && argNEllipsis {
			errs = append(errs, ErrInvalidEllipsisInCall{at(ctx, call)})
		}
	}
	return call, errs
}
