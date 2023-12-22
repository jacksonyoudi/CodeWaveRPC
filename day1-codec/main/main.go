package main

import (
	"cwrpc"
	"cwrpc/codec"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

func startServer(addr chan string) {
	// pick a free port
	//  启动一个 tcp conn
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	cwrpc.Accept(l)
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	// 启动服务
	go startServer(addr)

	// 客户端建立连接
	// in fact, following code is like a simple geerpc client
	conn, _ := net.Dial("tcp", <-addr)
	defer func() { _ = conn.Close() }()

	time.Sleep(time.Second)
	// send options
	_ = json.NewEncoder(conn).Encode(cwrpc.DefaultOption)
	cc := codec.NewGobCodec(conn)
	// send request & receive response
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			SeviceMethod: "Foo.Sum",
			Seq:          uint64(i),
		}
		// 向服务器写入数据 header, body
		_ = cc.Write(h, fmt.Sprintf("geerpc req %d", h.Seq))
		// 读取 响应header, 主要看 h.error
		_ = cc.ReadHeader(h)

		// 读取响应的 body
		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}
