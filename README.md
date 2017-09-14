## Go 实现线程池以及GracefulShutdown<br>Rust 实现线程池以及GracefulShutdown<br>及两者对比

### 线程池

- #### go

	``ThreadPool``结构中拥有以下元素：
	- ``volume`` 记录线程数，用以后面关闭线程时对已关闭的线程进行核对
	- ``workerQuue`` 用来放置闲置的线程，闲置的线程用来接收任务
	- ``jobs`` 用以接收等待处理的事物
	- ``jobsDone`` 用以接收已处理完的事物
 	- ``terminate`` 用来接收线程池关闭的信号，一旦收到关闭信号，则将所有线程关闭
	- ``sysSignal`` 用来接收终端 '^C' 信号，一旦接收到该信号，则先关闭线程池，之后再退出程序

	```go
	// 线程池，内部元素都不对外开放
	type ThreadPool struct {
		volume      int 
		workerQueue chan *worker
		jobs        chan *Job
		jobsDone    chan *Job 
		sysSignal   chan os.Signal
		terminate   chan struct{} 
	}
	```
- #### rust

	``ThreadPool``结构中拥有以下元素：
	- ``workers``一个向量，用来放置闲置的线程
	- ``sender``用来发送待处理的事件

	```rust
	pub struct ThreadPool {
		workers: Vec<Worker>,
		sender: mpsc::Sender<Message>,
	}
	```
	
	```rust
	impl ThreadPool {
		pub fn new(size: usize) -> ThreadPool {
			assert!(size > 0);
			let (sender, receiver) = mpsc::channel();
			let receiver = Arc::new(Mutex::new(receiver));
			let mut workers = Vec::with_capacity(size);
			for id in 0..size {
				workers.push(Worker::new(id, receiver.clone()));
			}
			ThreadPool { workers, sender }
    	}
		
		pub fn execute<F>(&self, f: F)
   		where
			F: FnOnce() + Send + 'static,
		{
			let job = Box::new(f);
			self.sender.send(Message::NewJob(job)).unwrap();
    	}
	}
	```


### 线程

- #### go
	- ``id`` 线程id

	```go
	// 一个线程
	type worker struct {
		id int
	}
	```
- #### rust
	- ``id``线程id
	- ``job``待处理的事物，一个``Option`` ``enum``，在``Option``中放置一个``Job``后，可以使用``Option``拥有的``take()``方法来获取一个``Job``的所有权，并将``Option``中的``Some()``变为``None``，实际上这是为了后面处理 graceful shutdown 所做的准备，否则这里直接放置一个``Job``类型就行了。``Option``中代码是这样的：

		```rust
			match {
				Some(T) => {
					let thread = T.take()
					// ... do something with 'thread'
				}
				None => {
				}
			}
		```

	``Worker``结构：
	
	```rust
	struct Worker {
	    id: usize,
	    job: Option<thread::JoinHandle<()>>,
	}
	```

### 事件

待处理的事件，需要外部实现其中的方法

- #### go ``Job``,``ErrorHandler``,``ResultHandler`` 接口
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
- #### rust ``Job`` ``Box``。``Job``同时拥有``FnBox``和``Send``两种特性

	```rust
	type Job = Box<FnBox + Send + 'static>;
	```
	
	- ``FnBox``特性拥有一个``call_box()``方法，用来调用``Job``函数本身

		```rust
		trait FnBox {
			fn call_box(self: Box<Self>);
		}
		```
	- 同时，``FnBox``只是一个``FnOnce()``特性的包装，即

		```rust
		impl<F: FnOnce()> FnBox for F {
			fn call_box(self: Box<F>) {
				(*self)()
			}
		}
		```
	
	- ``Send``特性用来发送


	

### go 事件返回值
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

- #### go

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

	- ``Treadpool`` 的 ``terminateWorkers`` 方法
		
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
- #### rust
	线程在处理事务时，首先会收到一个枚举类型``Message``：
	- ``NewJob(Job)``表示有任务需要处理
	- ``Terminate``表示该线程如果闲置的话，则停止接收任务

	```rust
	enum Message {
		NewJob(Job),
		Terminate,
	}
	```
	在收到``NewJob(Job)``时，直接使用``Job`` ``Box``中的``FnBox``特性所有的``call_box()``方法来执行任务；而收到``Terminate``时，不再接收任何``Message``。看起来应该是这样：
	
	```rust
	loop {
		method to receive a message
		let message = ...;
	
		match message {
			NewJob(job) => {
				job.call_box();
			}
			Terminate => {
				break;
			}
		}
	}
	```

	而``Message``通过包``mpsc``中的``channel()``方法返回的``sender``和``receiver``来发送和接收，同时，由于是一个线程发送任务，多个线程接收任务并处理，同时多个线程使用的是同一个``receiver``来接收任务。为了避免多个线程同时取到一个相同的任务去执行，需要在``receiver``上需要加锁来保证每次只有一个线程使用``receiver``来获取一个任务去执行：
	
	```rust
	let (sender, receiver) = mpsc::channel();
	
	let receiver = Arc::new(Mutex::new(receiver));
	
	let message = receiver.lock().unwrap().recv().unwrap();
	```
	
	最终，在``Worker``上实现如下的方法来让一个线程执行任务或者关闭：
	
	```rust
	impl Worker {
		pub fn new(id: usize, receiver: Arc<Mutex<mpsc::Receiver<Message>>>) -> Worker {
			let t = thread::spawn(move || loop {
				let message = receiver.lock().unwrap().recv().unwrap();

				match message {
					Message::NewJob(job) => {
						println!("Worker {} got a job; executing.", id);
						job.call_box();
					}
					Message::Terminate => {
						println!("Worker {} was told to terminate.", id);
						break;
					}
				}
				println!("Worker {} got a job; executing.", id);
			});
			
			Worker {
				id: id,
				job: Some(t),
			}
		}
	}
	```
### 线程池和线程的创建

- #### go
	- 新建线程池

		```go
		func newThreadPool() *ThreadPool {
			return new(ThreadPool)
		}
		```

	- 初始化线程池

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

- #### rust

	在``TreadPool``上实现了两个方法：
	
	```rust
	impl ThreadPool {
		pub fn new(size: usize) -> ThreadPool {
			assert!(size > 0);
	
			let (sender, receiver) = mpsc::channel();
		
			let receiver = Arc::new(Mutex::new(receiver));
		
			let mut workers = Vec::with_capacity(size);
	
			for id in 0..size {
				workers.push(Worker::new(id, receiver.clone()));
			}
	
			ThreadPool { workers, sender }
		}
	}
	```
	- ``new(unsize)``方法返回一个线程池，一个线程池中拥有``unsize``个``worker``和一个``sender``

### go 外部创建线程池方法

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

- #### go

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
- #### rust

	如果一个值拥有``Drop``特性，rust会在一个变量要离开其作用域时，调用该特性中的``drop()``方法。如果程序即将关闭，为了实现 graceful shutdown，就要保证最后离开作用域的应该是``TreadPool``本身，所以，在``TreadPool``上实现``Drop``特性
	
	```rust
	impl Drop for ThreadPool {
		fn drop(&mut self) {
		    println!("Sending terminate message to all workers");
		
		    for _ in &mut self.workers {
		        self.sender.send(Message::Terminate).unwrap();
		    }
		    println!("Shutting down all workers");
		
		    for worker in &mut self.workers {
		        println!("Shutting down worker {}", worker.id);
		
		        if let Some(thread) = worker.job.take() {
		            thread.join().unwrap();
		        }
		    }
		}
	}
	```

	当调用``drop()``时，首先发送和``worker``数量相同的``Terminate`` ``Message``。闲置的``worker``总是先收到``Terminate``，并从``receive message loop``中``break``；繁忙的``worker``在处理完``Job``之后会继续进入``receive message loop``，也能接收到``Terminate``，从而保证所有的``worker``都能收到``Terminate``并停止接收任何``Job``。在所有的``worker``都处理完事物之后，通过``join()``来依次关闭所有已打开的线程。这样就实现了graceful shutdown。
### 线程池处理事件

- #### go


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

- #### rust

	```rust
	pub fn execute<F>(&self, f: F)
		where
			F: FnOnce() + Send + 'static,
	{
		let job = Box::new(f);
		self.sender.send(Message::NewJob(job)).unwrap();
	}
	```
	- ``execute(F<FnOnce() + Send + 'static>)``方法发布一个具体的``Job``，前面的``Job``类型是``Box<FnBox + Send + 'static>``，而``FnBox``实际上是在``FnOnce()``特性上实现的另外一个特性，所以``FnBox``本身也具有``FnOnce()``特性，因此这里直接可以``execute(Job)``，新的``Job``被``sender``的``send``方法发送出去，由``worker``中的``receiver``来接收。


### 总结

实现线程池和GracefulShutdown的代码长度，两种语言都差不多长。go 的优势在于它的``interface{}``和``chan``+``gorutine``特别灵活，缺点在于对于类型的要求特别的确定，以及需要去处理错误和返回值。用go实现的多线程，差不多就像用一个办法简单的知道目前程序的闲置线程是否``>1``，如果是，直接对接收到的任务开一个``gorutine``去执行就完了.由于没有泛型，在传递``Job``的时候，需要显式的在一个特定的任务上实现所需求的各种方法。而rust的优势在于各种灵活的语言结构（``trait``,``enum``,``Box``,``Vec``等）以及拥有泛型，在GracefulShutdown上的处理也比用go处理起来更优雅，不用和go一样在``system.Interrupt``的时候还得亲自去捕捉一下并进行处理，只需要一个``Drop`` ``trait``就能搞定，缺点在于为了让程序本身能够编译通过所需要理顺的语言逻辑，远比这几行代码所需要的业务逻辑多的多。

### 例子
至此，线程池的处理事件方法已写完。外部调用时，应该是这样：

- #### go

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

- #### rust
	项目自带，略