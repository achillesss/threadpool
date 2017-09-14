## Go 实现线程池以及GracefulShutdown<br>Rust 实现线程池以及GracefulShutdown<br>及两者对比

### 线程池

```go
// 线程池，内部元素都不对外开放
type ThreadPool struct {
	// 用来接收线程池关闭的信号，一旦收到关闭信号，则将所有线程关闭
	terminate   chan struct{}  
	// 用来接收终端 '^C' 信号，一旦接收到该信号，则先关闭线程池，之后再退出程序
	sysSignal   chan os.Signal 
	// 用来放置闲置的线程，闲置的线程用来接收任务
	workerQueue chan *worker   
	// 记录线程数，用以后面关闭线程时对已关闭的线程进行核对
	volume      int
	// 用以接收任务、事件
	jobs        chan *Job
	// 用以接收已处理完的 job
	jobsDone    chan *Job
}
```
### 线程
```go
// 一个线程
type worker struct {
	id int
}
```

### 事件

待处理的事件，需要外部实现其中的方法

- 线程会在处理一个事件时使用 ``Do()`` 方法
- 通过``Error() error``来返回事件可能会出现的错误
- 通过``Result()interface{}``返回事件可能的返回值
- 如果实现了``HandleError``，则在处理事件时遇到了错误，会自动调用
- 如果实现了``HandleResult``，则在处理事件之后会自动调用

```go
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
```
### 事件返回值
传入一个事件和一个接收返回值的参数 ``result``，将事件的返回值赋值给 ``result``

```go
func Result(job Job, result interface{}) {
	t := reflect.TypeOf(result)
	if t.Kind() == reflect.Ptr {
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(job.Result()))
	}
}
```

### 事件处理逻辑
线程池中的闲置线程数为当前线程池能并发处理事件的最大能力，线程池首先监控 ``workerQueue``，大体上应该是这样：

```go
for w := range workerQueue {
	// worker 'w' start to work
}
```
接着，当线程池拿到一个闲置的 ``worker`` 时，此 ``worker`` 开始 ``working`` 的样子大概是这样：

```go
select {
	case <- terminate:
		workerQueue <- w
		// terminate TreadPool
	case job <- jobChan:
		// got a job and start to do it
		go job.Do()
		// after job is done
		workerQueue <- w
}
```
当线程池关闭某个线程时，实际上只需要简单的将某个线程移出闲置线程``channel``就行了：

```go
	w := <- workerQueue
	// w 被移出了
```

综合起来，就出现了下面的代码：

-  ``Treadpool`` 的 ``terminateWorkers`` 方法
	
	```go
	// 移出所有的闲置线程，直到数量为线程池的 volume 时，才结束
	func (t *ThreadPool) terminateWorkers() {
		for i := 0; i < t.volume; i++ {
			// fmt.Printf("%d workers remains to be terminated\n", t.volume-i)
			w := <-t.workerQueue
			// fmt.Printf("terminate worker %d\n", w.id)
		}
		// fmt.Printf("terminate finished.\n")
	}
	```

- ``worker`` 的 ``work`` 方法

	``work`` 方法实现了 graceful shutdown 的逻辑：关闭线程时，首先关闭所有闲置的线程，确保不再接收新的任务；其次，正在执行任务的线程在任务执行完毕之后被放回闲置线程队列中，然后被关闭

	```go
	func (w *worker) work(t *ThreadPool) {
		select {
		case <-t.terminate:
			// 如果是在获取了一个 worker 的情况下拿到了终止信号
			// 则将获取到的 worker 塞到闲置线程队列中，并直接调用终止线程池的方法
			t.workerQueue <- w
			t.terminateWorkers()
			// 传回终止信号是为了让外界知道此时线程池已经关闭
			t.terminate <- struct{}{}
		case job := <-t.jobs:
			// fmt.Printf("worker %d get a job\n", w.id)
			go func(t *ThreadPool, w *worker) {
				(*job).Do()
				if (*job).Error() != nil {
					// fmt.Printf("job got an error: %v\n", (*job).Error())
					if v, ok := (*job).(ErrorHandler); ok {
						// fmt.Printf("handle error\n")
						v.HandleError()
					}
				}
				if v, ok := (*job).(ResultHandler); ok {
					// fmt.Printf("handle result\n")
					v.HandleResult()
				}
				t.jobsDone <- job
				// fmt.Printf("worker %d has finished the job\n", w.id)
				t.workerQueue <- w
				// fmt.Printf("worker %d is waiting for a job\n", w.id)
			}(t, w)
		}
	}
	```

- ``TreadPool`` 的 ``deliveryJobs`` 方法
 
	```go
	func (t *ThreadPool) deliveryJobs() {
		// fmt.Printf("start to delivery jobs\n")
		for w := range t.workerQueue {
			w.work(t)
		}
	}
	```

### 线程池的创建

- 创建

	```go
	func newThreadPool() *ThreadPool {
		return new(ThreadPool)
	}
	```

- 初始化

	```go
	func (t *ThreadPool) init(volume int) *ThreadPool {
		t.jobs = make(chan Job)
		t.terminate = make(chan struct{})
		t.sysSignal = make(chan os.Signal)
		t.workerQueue = make(chan *worker, volume)
		t.volume = volume
	
		return t
	}
	```

### 线程的创建

- 创建一个线程

	```go
	func (t *ThreadPool) newWorker(id int) {
		w := new(worker)
		w.id = id
		t.workerQueue <- w
		fmt.Printf("create worker %d\n", w.id)
	}
	```

- 使用``gorutine``创建所有线程

	```go
	func (t *ThreadPool) createWorkers() *ThreadPool {
		restWorker := t.volume
		var group sync.WaitGroup
		group.Add(restWorker)
		for restWorker > 0 {
			restWorker--
			go func(restWorker int) {
				t.newWorker(t.volume - restWorker)
				group.Done()
			}(restWorker)
		}
		group.Wait()
		return t
	}
	
	```

### 外部创建线程池方法

```go
func NewThreadPool(volume int) *ThreadPool {
	if volume <= 0 {
		panic("invalid go rutine number")
	}
	t := newThreadPool().init(volume).createWorkers()
	go t.deliveryJobs()
	go t.close()
	return t
}
```

### 关闭线程池
线程有两种关闭的方法，一种是程序内正常关闭，此时向 ``TreadPool`` 中的 ``terminate`` 发送一个终止信号，在得到 ``terminate`` 中的信号返回时，确认线程池成功关闭；另外一种是捕获命令行的 ``^C`` 指令，此时调用关闭线程池的方法，在确认线程池关闭之后使用``Os.Exit(0)`` 来关闭程序

```go
func (t *ThreadPool) Close() {
	t.terminate <- struct{}{}
	<-t.terminate
}

func (t *ThreadPool) close() {
	signal.Notify(t.sysSignal, os.Interrupt)
	for {
		select {
		case <-t.sysSignal:
			t.Close()
			os.Exit(0)
		}
	}
}
```

### 线程池处理事件

```go
func (t *ThreadPool) Execute(j Job) {
	t.jobs <- j
	for job := range t.jobsDone {
		if job == j {
			break
		} else {
			t.jobsDone <- job
		}
	}
	// fmt.Printf("got a job\n")
}
```

### 例子
至此，线程池的处理事件方法已写完。外部调用时，应该是这样：

```go
// 假设这是我们要执行的函数
func HelloWorld(args ...interface{}) error {
	for i, arg := range args {
		time.Sleep(time.Second * 1)
		fmt.Printf("now, got arg %d: %+v\n", i, arg)
	}
	return fmt.Errorf("HelloWorld has no error")
}

// 错误处理函数
func handleErr(err error) {
	if err != nil {
		fmt.Printf("HandleError: JOB GOT AN ERROR: %v\n", err)
	} else {
		fmt.Printf("HandleError: JOB GOT NO ERROR\n")
	}
}

// 实现 Job
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

func (h *handler) Result(dst interface{}) {
	v := reflect.ValueOf(dst)
	t := reflect.TypeOf(dst)
	if t.Kind() != reflect.Ptr {
		return
	}
	v = v.Elem()
	t = t.Elem()
	v.Set(reflect.ValueOf(h.result))
	return
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
	pool.Result(h, &result)
	fmt.Printf("HandleResult: GOT RESULT: %q\n", result)
}

func newJob(args ...interface{}) pool.Job {
	return &handler{
		args: args,
	}
}

func main() {
	// serve()
	var threadPoolVolume = 10
	threadPool := pool.NewThreadPool(threadPoolVolume)
	defer threadPool.Close()

	job := newJob("one", "two", "three")
	threadPool.Execute(&job)
	fmt.Printf("job \"one, two, three\" finished!\n")
}
```

如果没有关闭代码中的 ``fmt.Printf``，将会看到整个程序打印出来的执行流程，类似如下：

```bash
create worker 6
create worker 2
create worker 3
create worker 4
create worker 8
create worker 7
create worker 1
create worker 9
create worker 5
create worker 10
got a job: 0xc420078fe0
start to delivery jobs
worker 2 get a job: 0xc420078fe0
now, got arg 0: one
now, got arg 1: two
now, got arg 2: three
job got an error: HelloWorld has no error
handle error
HandleError: JOB GOT AN ERROR: HelloWorld has no error
handle result
HandleResult: GOT RESULT: "one"
worker 2 has finished the job
worker 2 is waiting for a job
got job 0xc420078fe0 done
job "one, two, three" finished!
start to close thread pool
10 workers remains to be terminated
terminate worker 6
9 workers remains to be terminated
terminate worker 3
8 workers remains to be terminated
terminate worker 4
7 workers remains to be terminated
terminate worker 8
6 workers remains to be terminated
terminate worker 7
5 workers remains to be terminated
terminate worker 1
4 workers remains to be terminated
terminate worker 9
3 workers remains to be terminated
terminate worker 5
2 workers remains to be terminated
terminate worker 2
1 workers remains to be terminated
terminate worker 10
terminate workers finished.
close thread pool finished
```