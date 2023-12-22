package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

// 定义Gob解码器

type GobCodec struct {
	conn io.ReadWriteCloser // 网络conn
	buf  *bufio.Writer      // buf 缓冲
	dec  *gob.Decoder       //  解码器
	enc  *gob.Encoder       // 编码器
}

// 这行代码是 Go 语言中的一种类型断言的写法，用于检查 `*GobCodec` 类型是否实现了 `Codec` 接口。
// 在这行代码中，`var _ Codec` 定义了一个匿名变量，类型为 `Codec` 接口。然后，`(*GobCodec)(nil)` 是一个类型为 `*GobCodec` 的空指针。
// 通过将空指针赋值给匿名变量，编译器会在编译时检查 `*GobCodec` 类型是否实现了 `Codec` 接口。如果 `*GobCodec` 类型没有实现 `Codec` 接口，编译器会在编译时报错。
// 这种写法通常用于确保某个类型实现了特定的接口，以避免在运行时出现错误。如果编译通过，说明 `*GobCodec` 类型确实实现了 `Codec` 接口。
var _ Codec = (*GobCodec)(nil)

// 创建一个code对象
func NewGobCodec(conn io.ReadWriteCloser) Codec {
	// 用于缓冲
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

// 实现codec接口
func (g *GobCodec) Close() error {
	return g.conn.Close()
}

func (g *GobCodec) ReadHeader(header *Header) error {
	// 从 conn读取数据出来, 将数据从conn读取出来后, 解码后数据放在header中
	return g.dec.Decode(header)
}

func (g *GobCodec) ReadBody(i interface{}) error {
	// 将数据从conn读取出来,解码后数据写到i中
	return g.dec.Decode(i)
}

// 将header和body数据写入到 conn中, 数据线写入一个buf缓冲中, 然后在写入 conn中的
func (g *GobCodec) Write(header *Header, i interface{}) (err error) {
	// 压入栈中
	defer func() {
		// 将数据刷写到 conn中
		_ = g.buf.Flush()
		if err != nil {
			_ = g.Close()
		}
	}()

	// 数据先写header, 然后在写body
	if err = g.enc.Encode(header); err != nil {
		log.Println("rpc: gob error encoding header:", err)
		return
	}

	if err = g.enc.Encode(i); err != nil {
		log.Println("rpc: gob error encoding body:", err)
		return
	}
	return

}

// todo
// 应该将读 header和body放在一起, 当成一个原子操作, 这样每次获取一个请求
