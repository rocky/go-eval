package eval

import (
	"reflect"
)

func evalStarExpr(ctx *Ctx, starExpr *StarExpr, env *Env) (reflect.Value, error) {
	if vs, _, err := EvalExpr(ctx, starExpr.X.(Expr), env); err != nil {
		return reflect.Value{}, err
	} else {
		v := (*vs)[0]
		if v.IsNil() {
			return reflect.Value{}, PanicInvalidDereference{}
		}
		return v.Elem(), nil
	}
}
