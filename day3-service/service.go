package cwrpc

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

// 定义methodType struct A.hello(x) y
type methodType struct {
	method    reflect.Method
	ArgType   reflect.Type
	ReplyType reflect.Type
	numCalls  uint64 // 统计方法调用次数时会用到
}

// 统计方法调用次数时会用到

func (method *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&method.numCalls)
}

// 创建 method的 输入参数
// 根据 类型, 创建一个  输入参数的实例
//
//	TypeOf(x).Type && Kind()
//
// 区分指针和普通类型,
// 如果是指针, 需要获取指针对应的类型,
func (method *methodType) newArgv() reflect.Value {
	var argv reflect.Value

	if method.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(method.ArgType.Elem())
	} else {
		argv = reflect.New(method.ArgType).Elem()
	}
	return argv
}

// 创建 method的 reply的实例
func (method *methodType) newReplyv() reflect.Value {
	// 指针类型的
	replyv := reflect.New(method.ReplyType.Elem())
	// 区分类型, 将 method的, 值赋值过去
	switch method.ReplyType.Elem().Kind() {
	// map
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(method.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(method.ReplyType.Elem(), 0, 0))
	}

	return replyv
}

// 定义service 结构体
type service struct {
	// 名称
	name string
	// 服务类型
	typ reflect.Type
	// 结构体的实例本身
	rcvr reflect.Value

	//存储映射的结构体的所有符合条件的方法
	method map[string]*methodType
}

// 创建一个服务,
func newService(rcvr interface{}) *service {
	s := new(service)
	// 结构体对应的value
	s.rcvr = reflect.ValueOf(rcvr)
	//  结构体对应的 name
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	// 类型
	s.typ = reflect.TypeOf(rcvr)
	// 如果不是一个可导出的类型, 就失败
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	// 注册 方法
	s.registerMethods()
	return s
}

func (s *service) registerMethods() {
	// 定义一个method map
	s.method = make(map[string]*methodType)

	// 获取类型的所有方法
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		// 如果输入参数 不是 3个,返回值是1个
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		// 如果 返回值是error, 就跳过
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}

		// 输入的类型 methodservice, argType, replyType
		argType, replyType := mType.In(1), mType.In(2)
		// 不可用的方法,跳过
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}

		// 加入到sevice method方法中
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

// service的call实现
func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	// 记录多少个calls
	atomic.AddUint64(&m.numCalls, 1)

	// 对应的函数
	f := m.method.Func
	// 函数进行调用, 并返回结果
	// service函数的格式都定义成 A.B(argv,reply) error 格式
	returnValues := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}
