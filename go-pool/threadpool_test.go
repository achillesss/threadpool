package pool

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"
)

type handler struct {
	args   []interface{}
	result string
	err    error
}

func (h *handler) Do() {
	h.result, h.err = HelloWorld(h.args...)
}

func (h *handler) Error() error {
	return h.err
}

func (h *handler) Result() interface{} {
	return h.result
}

func (h *handler) HandleError() {
	if h.err != nil {
		fmt.Printf("HandleError: JOB GOT AN ERROR: %v\n", h.err)
	} else {
		fmt.Printf("HandleError: JOB GOT NO ERROR\n")
	}
}

func (h *handler) HandleResult() {
	var result string
	Result(h, &result)
	fmt.Printf("HandleResult: GOT RESULT: %q\n", result)
}

func HelloWorld(args ...interface{}) (string, error) {
	for i, arg := range args {
		time.Sleep(time.Second * 1)
		fmt.Printf("now, got arg %d: %+v\n", i, arg)
	}
	return fmt.Sprintf("%v", args[0]), fmt.Errorf("HelloWorld has no error")
}

func newJob(args ...interface{}) Job {
	return &handler{
		args: args,
	}
}

func testHelloWorld(t *testing.T) {
	var threadPoolVolume = 10
	threadPool := NewThreadPool(threadPoolVolume)
	defer threadPool.Close()

	job := newJob("one", "two", "three")
	threadPool.Execute(&job)
	fmt.Printf("job \"one, two, three\" finished!\n")
}

func testTCP(t *testing.T) {
	addr := net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8107}
	l, err := net.ListenTCP("tcp", &addr)
	if err != nil {
		panic(err)
	}
	defer l.Close()

	fmt.Printf(`type "echo -n "test out the server" | nc localhost `+"%d\" in command-line\n", addr.Port)
	threadPool := NewThreadPool(4)
	defer threadPool.Close()

	for {
		conn, err := l.AcceptTCP()
		if err != nil {
			panic(err)
		}
		job := newTCPJob(conn)
		go threadPool.Execute(&job)
	}
}

func newTCPJob(tcpConn *net.TCPConn) Job {
	return &tcpHandler{tcpConn}
}

type tcpHandler struct {
	conn *net.TCPConn
}

func (h *tcpHandler) Do() {
	handleRequest(h.conn)
}

func (h *tcpHandler) Error() error {
	return nil
}

func (h *tcpHandler) Result() interface{} {
	return nil
}

func handleRequest(conn *net.TCPConn) {
	data, err := ioutil.ReadAll(conn)
	if err != nil {
		panic(fmt.Errorf("Error reading: %s", err.Error()))
	}
	time.Sleep(time.Second * 5)
	fmt.Printf("request: %s\n", data)
	_, err = conn.Write([]byte("Message received."))
	if err != nil {
		panic(err)
	}

	defer conn.Close()
	return
}

func shutdown(t *testing.T) {
	fmt.Printf("\033[31;1mTEST WILL SHUTDOWN IN 30s\033[0m\n")
	time.Sleep(time.Second * 30)
	os.Exit(0)
}
func Test(t *testing.T) {
	go shutdown(t)
	testHelloWorld(t)
	testTCP(t)
}
