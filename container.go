package dic

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
)

type _DiFnc struct {
	deps []reflect.Type
	fnc  reflect.Value
	done bool
}

func (ele *_DiFnc) call(args []reflect.Value) (rvs []reflect.Value, ok bool) {
	defer func() {
		rv := recover()
		if rv == nil {
			return
		}
		e, iserr := rv.(error)
		if iserr && IsTokenNotFound(e) {
			ok = false
			rvs = nil
			return
		}
		panic(rv)
	}()
	return ele.fnc.Call(args), true
}

type Container[T comparable] struct {
	execed       bool
	fncs         []*_DiFnc
	asyncfuncs   []*_DiFnc
	valpool      map[reflect.Type]reflect.Value
	tokenvalpool map[reflect.Type]map[T]reflect.Value
}

func New[T comparable]() *Container[T] {
	return &Container[T]{
		valpool:      make(map[reflect.Type]reflect.Value),
		tokenvalpool: make(map[reflect.Type]map[T]reflect.Value),
	}
}

func (dic *Container[T]) appendone(v reflect.Value) {
	var vtype reflect.Type
	var maytoken sql.Null[T]
	if v.Type().Implements(tokenValueInterfaceType) {
		var anytoken any
		anytoken, vtype, v = (v.Interface().(__I_private_dic_token_value__)).__private_dic_token_value__()
		maytoken = sql.Null[T]{V: anytoken.(T), Valid: true}
	} else {
		vtype = v.Type()
	}

	if maytoken.Valid {
		tvm := dic.tokenvalpool[vtype]
		if tvm == nil {
			tvm = make(map[T]reflect.Value)
		}
		_, ok := tvm[maytoken.V]
		if ok {
			panic(fmt.Errorf("dic: type `%v`, token `%v` is already registered", vtype, maytoken.V))
		}
		tvm[maytoken.V] = v
		dic.tokenvalpool[vtype] = tvm
		return
	}

	_, ok := dic.valpool[vtype]
	if ok {
		panic(fmt.Errorf("dic: `%s` is already registered", vtype))
	}
	dic.valpool[vtype] = v
}

func (dic *Container[T]) append(v reflect.Value) {
	if v.Kind() == reflect.Slice && v.Type().Elem().Implements(tokenValueInterfaceType) {
		for i := range v.Len() {
			dic.appendone(v.Index(i))
		}
		return
	}
	dic.appendone(v)
}

func (dic *Container[T]) get(k reflect.Type) (reflect.Value, bool) {
	v, ok := dic.valpool[k]
	return v, ok
}

type errTokenNotFound struct {
	rtype reflect.Type
	token any
}

func (e *errTokenNotFound) Error() string {
	return fmt.Sprintf("dic: type `%s` token `%s` not found", e.rtype, e.token)
}

var _ error = (*errTokenNotFound)(nil)

func IsTokenNotFound(e error) bool {
	_, ok := e.(*errTokenNotFound)
	return ok
}

func (dic *Container[T]) getbytoken(token T, k reflect.Type) reflect.Value {
	tvm, ok := dic.tokenvalpool[k]
	if !ok {
		panic(&errTokenNotFound{token: token, rtype: k})
	}
	v, ok := tvm[token]
	if !ok {
		panic(&errTokenNotFound{token: token, rtype: k})
	}
	return v
}

func (dic *Container[T]) mkfnc(fnc any) *_DiFnc {
	rv := reflect.ValueOf(fnc)
	if rv.IsNil() || rv.Kind() != reflect.Func {
		panic(fmt.Errorf("dic: `%s` is not a function", fnc))
	}

	ele := &_DiFnc{fnc: rv}
	for i := range rv.Type().NumIn() {
		argtype := rv.Type().In(i)
		ele.deps = append(ele.deps, argtype)
	}
	return ele
}

var (
	ErrContainerAlreadyExecuted = errors.New("dic: container already executed")
)

func (dic *Container[T]) Register(fnc any) *Container[T] {
	if dic.execed {
		panic(ErrContainerAlreadyExecuted)
	}
	dic.fncs = append(dic.fncs, dic.mkfnc(fnc))
	return dic
}

func (dic *Container[T]) Try(fnc any) *Container[T] {
	ele := dic.mkfnc(fnc)
	if dic.execAsync(ele) {
		return dic
	}

	if dic.execed {
		panic(ErrContainerAlreadyExecuted)
	}
	dic.asyncfuncs = append(dic.asyncfuncs, ele)
	return dic
}

func (dic *Container[T]) GetByToken(dest any, token T) *Container[T] {
	dv := reflect.ValueOf(dest)
	dt := dv.Type().Elem()
	val := dic.getbytoken(token, dt)
	dv.Elem().Set(val)
	return dic
}

func (dic *Container[T]) execAsync(ele *_DiFnc) bool {
	if ele.done {
		return false
	}
	args := dic.deps4ele(ele)
	if len(args) == len(ele.deps) {
		rvs, ok := ele.call(args)
		if !ok {
			return false
		}
		for _, rv := range rvs {
			rev, ok := rv.Interface().(error)
			if ok {
				panic(rev)
			}
		}
		ele.done = true
		return true
	}
	return false
}

func (dic *Container[T]) execAllAsyncEles() {
	for _, ele := range dic.asyncfuncs {
		dic.execAsync(ele)
	}
}

func (dic *Container[T]) deps4ele(ele *_DiFnc) []reflect.Value {
	var args []reflect.Value
	for _, argtype := range ele.deps {
		av, ok := dic.get(argtype)
		if !ok {
			break
		}
		args = append(args, av)
	}
	return args
}

func (dic *Container[T]) Run() {
	if dic.execed {
		panic(ErrContainerAlreadyExecuted)
	}
	dic.execed = true

	for {
		var remains []*_DiFnc
		for _, ele := range dic.fncs {
			if ele.done {
				continue
			}
			remains = append(remains, ele)
		}

		if len(remains) < 1 {
			break
		}

		donecount := 0
		for _, ele := range remains {
			args := dic.deps4ele(ele)
			if len(args) == len(ele.deps) {
				rvs, ok := ele.call(args)
				if !ok {
					continue
				}
				for _, rv := range rvs {
					rev, ok := rv.Interface().(error)
					if ok {
						panic(rev)
					}
					dic.append(rv)
				}
				ele.done = true
				donecount++
			}
		}

		if donecount < 1 {
			fmt.Println("Remaining functions that could not be executed due to missing dependencies:")
			for _, ele := range remains {
				if ele.done {
					continue
				}
				fnName := runtime.FuncForPC(ele.fnc.Pointer()).Name()
				fmt.Printf("  - Function: %s\n", fnName)
			}
			panic(fmt.Errorf("dic: cannot resolve dependencies. Some functions could not be executed due to missing tokens"))
		}
		dic.execAllAsyncEles()
	}

	dic.execAllAsyncEles()

	udi := slices.IndexFunc(dic.asyncfuncs, func(ele *_DiFnc) bool { return !ele.done })
	if udi > -1 {
		fmt.Println("Remaining async-functions that could not be executed due to missing dependencies:")
		for _, ele := range dic.asyncfuncs {
			if ele.done {
				continue
			}
			fnName := runtime.FuncForPC(ele.fnc.Pointer()).Name()
			fmt.Printf("  - Function: %s\n", fnName)
		}
		panic(fmt.Errorf("dic: cannot resolve dependencies. Some functions could not be executed due to missing tokens"))
	}
}
