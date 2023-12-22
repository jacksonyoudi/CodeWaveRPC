package cwrpc

import (
	"cwrpc/codec"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
)

// 定义一个魔数
const MagicNumber = 0x3bef5c

// option
type Option struct {
	MagicNumber int
	CodecType   codec.Type
}

// 默认编码格式
var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}

type Server struct {
}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() { _ = conn.Close() }()
	var opt Option
	// 先将 编码数据写到 conn
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	//  简单验证
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}

	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}

	server.serveCodec(f(conn))
}

var invalidRequest = struct{}{}

// 相当于启动一个服务
//
//	网络连接包装到 codec中了
func (server *Server) serveCodec(cc codec.Codec) {
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		// req是包含 header和argv参数
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break // it's not possible to recover, so close the connection
			}
			// 将 error放到 header中
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg)
	}
	wg.Wait()
	_ = cc.Close()
}

type request struct {
	h            *codec.Header
	argv, replyv reflect.Value
}

func (s *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	// 从conn中读出 header
	if err := cc.ReadHeader(&h); err != nil {
		// 如果结束或异常, 就 返回
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

func (s *Server) readRequest(cc codec.Codec) (*request, error) {
	//  读取出请求头
	h, err := s.readRequestHeader(cc)

	if err != nil {
		return nil, err
	}
	// 构造 request
	req := &request{h: h}

	// 请求参数
	req.argv = reflect.New(reflect.TypeOf(""))
	// 读取出 body写到 argv中
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv err:", err)
	}
	return req, nil

}

// 服务端返回响应
func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	// 将header和 body写到conn中
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	// TODO, should call registered rpc methods to get the right replyv
	// day 1, just print argv and send a hello message
	defer wg.Done()
	log.Println(req.h, req.argv.Elem())
	// 响应的结果
	req.replyv = reflect.ValueOf(fmt.Sprintf("geerpc resp %d", req.h.Seq))
	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}

// 启动一个服务
func (server *Server) Accept(lis net.Listener) {
	for {
		// 接收,建立一个conn
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		// 处理一个conn
		go server.ServeConn(conn)
	}
}

// Accept accepts connections on the listener and serves requests
// for each incoming connection.
func Accept(lis net.Listener) { DefaultServer.Accept(lis) }
