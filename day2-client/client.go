package cwrpc

import (
	"cwrpc/codec"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

// 构造调用的 数据结构
// Call represents an active RPC.
type Call struct {
	Seq           uint64
	ServiceMethod string
	Args          interface{}
	Reply         interface{}
	Error         error
	Done          chan *Call // Strobes when call is complete.
}

// 自己给自己
// 为了支持异步调用，Call 结构体中添加了一个字段 Done，Done 的类型是 chan *Call，当调用结束时，会调用 call.done() 通知调用方。
func (c *Call) done() {
	c.Done <- c
}

// client数据结构
type Client struct {
	cc       codec.Codec // 编码
	opt      *Option     // 定义编码类型
	sending  sync.Mutex
	header   codec.Header // header (method seq error)
	mu       sync.Mutex
	seq      uint64 // 序号
	pending  map[uint64]*Call
	closing  bool
	shutdown bool
}

// 为了验证 client实现了接口 io.Closer
var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connection is shut down")

// 所有操作都加锁
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closing {
		return ErrShutdown
	}

	// client关闭
	client.closing = true
	return client.cc.Close()
}

// 判定client是否是活跃的状态
func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.shutdown && !client.closing
}

// 向client 注册call
func (client *Client) registerCall(call *Call) (uint64, error) {
	// 保证顺序
	client.mu.Lock()
	defer client.mu.Unlock()

	// 判定是否关闭
	if client.shutdown || client.closing {
		return 0, ErrShutdown
	}
	// client控制顺序
	call.Seq = client.seq
	// client保存 一个pending的 call的map
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

// todo 需要验证是否可以获取，然后删除的一些边界判断
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()

	// 根据 seq从pending中移除call
	call := client.pending[seq]

	delete(client.pending, seq)
	return call
}

// 关闭所有call
func (client *Client) terminateCalls(err error) {
	//  等 sending结束
	client.sending.Lock()
	defer client.sending.Unlock()
	//  保证顺序
	client.mu.Lock()
	defer client.mu.Unlock()

	client.shutdown = true

	//  挨个erro, 设置done, 然后关闭
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

// 发送 call
func (client *Client) send(call *Call) {
	//  控制sending顺序
	client.sending.Lock()
	defer client.sending.Unlock()

	//先注册, 加入 pending中, 并返回一个seq
	seq, err := client.registerCall(call)
	if err != nil {
		// 失败
		call.Error = err
		call.done()
		return
	}

	//  设置header
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	//  发送 header和argv
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		//  如果失败, 需要remove, TODO 这里其实需要判定
		call := client.removeCall(seq)
		// call may be nil, it usually means that Write partially failed,
		// client has received the response and handled
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		// 读取 header, error是响应的error
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}

		//正常响应了, call算 done
		call := client.removeCall(h.Seq)
		switch {
		case call == nil:
			// it usually means that Write partially failed
			// and call was already removed.
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			//  返回有error
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			// 将响应结构保存到 call.reply上
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}
	// error occurs, so terminateCalls pending calls
	client.terminateCalls(err)
}

// Go invokes the function asynchronously.
// It returns the Call structure representing the invocation.
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	// 设置一个 chan, 用于处理done的call
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		// 最多处理10个
		log.Panic("rpc client: done channel is unbuffered")
	}
	// 构造一个 Call
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	// 发送到服务端
	client.send(call)
	return call
}

// Call invokes the named function, waits for it to complete,
// and returns its error status.
func (client *Client) Call(serviceMethod string, args, reply interface{}) error {
	// Done 一个chan用于处理 完成的call,
	call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error
}

func parseOptions(opts ...*Option) (*Option, error) {
	// if opts is nil or pass nil as parameter
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}
	// send options with server
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error: ", err)
		_ = conn.Close()
		return nil, err
	}

	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *Option) *Client {
	// 构建client
	client := &Client{
		seq:     1, // seq starts with 1, 0 means invalid call
		cc:      cc,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	// 后台接收服务端的响应
	go client.receive()
	return client
}

// Dial connects to an RPC server at the specified network address
func Dial(network, address string, opts ...*Option) (client *Client, err error) {
	//
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	//  建立conn
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	// close the connection if client is nil
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	return NewClient(conn, opt)
}
