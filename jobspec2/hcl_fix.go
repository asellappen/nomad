package jobspec2

import (
	"fmt"
	"math"
	"math/big"
	"reflect"

	"github.com/hashicorp/hcl/v2"
	"github.com/mitchellh/reflectwalk"
	"github.com/zclconf/go-cty/cty"
)

func fixMapInterfaceType(v interface{}, ctx *hcl.EvalContext) hcl.Diagnostics {
	w := &walker{ctx: ctx}
	err := reflectwalk.Walk(v, w)
	if err != nil {
		w.diags = append(w.diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "unexpected internal error",
			Detail:   err.Error(),
		})
	}
	return w.diags
}

type walker struct {
	ctx   *hcl.EvalContext
	diags hcl.Diagnostics
}

var mapStringInterfaceType = reflect.TypeOf(map[string]interface{}{})

func (w *walker) Map(m reflect.Value) error {
	if !m.Type().AssignableTo(mapStringInterfaceType) {
		return nil
	}

	for _, k := range m.MapKeys() {
		v := m.MapIndex(k)
		if attr, ok := v.Interface().(*hcl.Attribute); ok {
			c, diags := decodeInterface(attr.Expr, w.ctx)
			w.diags = append(w.diags, diags...)

			m.SetMapIndex(k, reflect.ValueOf(c))
		}
	}
	return nil
}

func (w *walker) MapElem(m, k, v reflect.Value) error {
	return nil
}
func decodeInterface(expr hcl.Expression, ctx *hcl.EvalContext) (interface{}, hcl.Diagnostics) {
	srvVal, diags := expr.Value(ctx)

	dst, err := interfaceFromCtyValue(srvVal)
	if err != nil {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "unsuitable value type",
			Detail:   fmt.Sprintf("Unsuitable value: %s", err.Error()),
			Subject:  expr.StartRange().Ptr(),
			Context:  expr.Range().Ptr(),
		})
	}
	return dst, diags
}

func interfaceFromCtyValue(val cty.Value) (interface{}, error) {
	t := val.Type()
	//if val.IsMarked() {
	//	return fmt.Errorf("value has marks, so it cannot be serialized as JSON")
	//}

	//// If we're going to decode as DynamicPseudoType then we need to save
	//// dynamic type information to recover the real type.
	//if t == cty.DynamicPseudoType && val.Type() != cty.DynamicPseudoType {
	//	return marshalDynamic(val, path, b)
	//}

	if val.IsNull() {
		return nil, nil
	}

	if !val.IsKnown() {
		return nil, fmt.Errorf("value is not known")
	}

	// The caller should've guaranteed that the given val is conformant with
	// the given type t, so we'll proceed under that assumption here.

	switch {
	case t.IsPrimitiveType():
		switch t {
		case cty.String:
			return val.AsString(), nil
		case cty.Number:
			if val.RawEquals(cty.PositiveInfinity) {
				return math.Inf(1), nil
			} else if val.RawEquals(cty.NegativeInfinity) {
				return math.Inf(-1), nil
			} else {
				return smallestNumber(val.AsBigFloat()), nil
			}
		case cty.Bool:
			return val.True(), nil
		default:
			panic("unsupported primitive type")
		}
	case t.IsListType(), t.IsSetType(), t.IsTupleType():
		result := []interface{}{}

		it := val.ElementIterator()
		for it.Next() {
			_, ev := it.Element()
			evi, err := interfaceFromCtyValue(ev)
			if err != nil {
				return nil, err
			}
			result = append(result, evi)
		}
		return result, nil
	case t.IsMapType():
		result := map[string]interface{}{}
		it := val.ElementIterator()
		for it.Next() {
			ek, ev := it.Element()

			ekv := ek.AsString()
			evv, err := interfaceFromCtyValue(ev)
			if err != nil {
				return nil, err
			}

			result[ekv] = evv
		}
		return []map[string]interface{}{result}, nil
		//	case t.IsTupleType():
		//		b.WriteRune('[')
		//		etys := t.TupleElementTypes()
		//		it := val.ElementIterator()
		//		path := append(path, nil) // local override of 'path' with extra element
		//		i := 0
		//		for it.Next() {
		//			if i > 0 {
		//				b.WriteRune(',')
		//			}
		//			ety := etys[i]
		//			ek, ev := it.Element()
		//			path[len(path)-1] = cty.IndexStep{
		//				Key: ek,
		//			}
		//			err := marshal(ev, ety, path, b)
		//			if err != nil {
		//				return err
		//			}
		//			i++
		//		}
		//		b.WriteRune(']')
		//		return nil
	case t.IsObjectType():
		result := map[string]interface{}{}

		for k := range t.AttributeTypes() {
			av := val.GetAttr(k)
			avv, err := interfaceFromCtyValue(av)
			if err != nil {
				return nil, err
			}

			result[k] = avv
		}
		return []map[string]interface{}{result}, nil
	case t.IsCapsuleType():
		rawVal := val.EncapsulatedValue()
		return rawVal, nil
	default:
		// should never happen
		return nil, fmt.Errorf("cannot serialize %s", t.FriendlyName())
	}
}

func smallestNumber(b *big.Float) interface{} {

	if v, acc := b.Int64(); acc == big.Exact {
		// check if it fits in int
		if int64(int(v)) == v {
			return int(v)
		}
		return v
	}

	if v, acc := b.Float64(); acc == big.Exact || acc == big.Above {
		return v
	}

	return b
}
