package pool

import "os"

// TreadPool
type ThreadPool struct {
	terminate   chan struct{}  // 关闭线程池
	sysSignal   chan os.Signal // 捕获 Ctrl+C
	workerQueue chan *worker   // 闲置资源
	volume      int            // 线程数
	jobs        chan *Job      // 等待处理的事件集；没有缓冲
	jobsDone    chan *Job
}

type worker struct {
	id int
}

// Job to be done
type Job interface {
	Do()
	Result() interface{}
	Error() error
}

type ErrorHandler interface {
	HandleError()
}

type ResultHandler interface {
	HandleResult()
}
