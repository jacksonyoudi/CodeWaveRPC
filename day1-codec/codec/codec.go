package codec

import "io"

/// 定义请求头

type Header struct {
	SeviceMethod string // 方法 User.SayHello 这种
	Seq          uint64 //请求的序列号，为了区分不同请求的
	Error        string // 错误信息
}

// 定义编码的接口

type Codec interface {
	// 接口必须实现Close() 函数
	io.Closer
	// 读出 Header
	ReadHeader(*Header) error
	// 读出 body
	ReadBody(interface{}) error

	// 写入 header和 body
	Write(*Header, interface{}) error
}

// 定义类型， 是一个函数， 函数签名如下
type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json"
)

var NewCodecFuncMap map[Type]NewCodecFunc

// 调用这个包的时候， 就会执行
func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}

/// 编码 header + body
/// header (method, seq, error)
