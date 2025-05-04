package dic

import "reflect"

type __I_private_dic_token_value__ interface {
	__private_dic_token_value__() (any, reflect.Type, reflect.Value)
}

var tokenValueInterfaceType = reflect.TypeOf((*__I_private_dic_token_value__)(nil)).Elem()

type TokenValue[T comparable] struct {
	token T
	val   any
}

func (tv TokenValue[T]) __private_dic_token_value__() (any, reflect.Type, reflect.Value) {
	vv := reflect.ValueOf(tv.val)
	return tv.token, vv.Type(), vv
}

var (
	_ __I_private_dic_token_value__ = TokenValue[int]{}
)

func ValueWithToken[T comparable](token T, val any) TokenValue[T] {
	return TokenValue[T]{token: token, val: val}
}
