## Go 实现线程池以及GracefulShutdown<br>Rust 实现线程池以及GracefulShutdown<br>及两者对比

在并发处理任务时，往往需要对线程数量进行控制，同时需要在关闭程序之前能够处理完所有正在处理的任务，所以对于拥有GracefulShutdown功能的线程池的需求十分明显。下面分别介绍使用``go``和``rust``实现以上线程池的过程。

大致的思路差不多是这样的：

1. 我们需要一个外部的入口，来生成一个指定数量的线程池：
```
var threadPool = NewThreadPool(volume);
```
2. 同时需要另外一个方法能够将一个任意的任务丢到线程池中处理：
```
var job = newJob();
threadPool.Execute(job);
```
3. 线程池自身能够实现``GracefulShutdown``，即在关闭程序之前能够处理完所有正在处理的任务
4. 如果有需要，我们应该可以在任务处理完的同时，很方便的处理任务的返回值及错误

### go 实现线程池

首先，我们需要一个定制一个一定线程数量的线程池的函数：

```go
	func NewThreadPool(volume int) *ThreadPool {
		threadPool := new(ThreadPool)

		// threadPool 初始化

		return threadPool
	}
```

一个``ThreadPool``应该是这样的：
1. 具有一个容量``volume``
2. 拥有一个存放闲置线程的结构，从中获取闲置线程以执行任务，并在一个线程执行完任务之后，能很方便的将该线程放置回去等待其他任务
3. 拥有一个接收任务的``channel``
4. 拥有一个接信号的``channel``来关闭线程池
5. 一个监测系统信号``Interrupt``的``channel``

因此：
```go
type ThreadPool struct {
	// 容量
	volume      int 
	// 闲置线程队列
	workerQueue chan *worker
	// 接收任务
	jobs        chan Job
	// 监测'Interrupt'信号
	sysSignal   chan os.Signal
	// 监测关闭线程池信号
	terminate   chan struct{} 
}
```

于是，创建新线程池的第一步就是给新的线程池分配一个内存：
```go
func newThreadPool() *ThreadPool {
	return new(ThreadPool)
}
```

之后便是对拿到的线程池进行初始化：
```go
func (t *ThreadPool) init(volume int) *ThreadPool {
	t.volume = volume
	t.workerQueue = make(chan *worker)
	t.jobs = make(chan Job)
	t.sysSignal = make(chan os.Signal)
	t.terminate = make(chan struct{})
	return t
}
```

接下来要考虑的是单个线程的问题。``go``的并发执行任务非常简单，不需要我们去用某种方法获取到一个线程，然后在线程里面执行某个函数，之后再将这个线程关闭。我们只需要在一个函数前面加上``go ``，程序便会自动的新开一个``gorutine``来并发的执行这个函数，并且在函数执行完毕之后关闭该``gorutine``。所以，单个线程的结构并不复杂，只需要它自己记录一个自己的``id``就行了：

```go
type worker struct {
	id int
}
```

然后是需要被执行的任务``Job``。我们希望任何可执行的任务都能被线程池接收并执行，所以这个``Job``在``go``里面如果是一个``interface{}``的话，会是非常方便的。同时还要考虑到下面的一些点：
1. 任务的类型变成了``interface{}``，如果要执行它，那么这个``interface{}``一定要有一个执行的方法``Do()``
2. 任务有可能是有返回值和错误的，所以，最好在``Job``中保留获取返回值``Result``和获取错误``Error``的方法
3. 有可能任务还需要在执行完毕之后顺带处理它的返回值或者结果，所以可以提供两个方法``HandleResult``和``HandleError``，在``Job.Do()``之后得到返回值或者错误之后使用
4. 每个任务的返回值的类型有可能都不一样，所以，还需要一个函数来正确的拿到某个任务指定类型的返回值，而不是一个``interface{}``

	首先，下面定义了一个``Job``接口：

	```go
	type Job interface{
		Do()
		Result() interface{}
		Error() error
	}
	```

	这样，在一个``Job``传递进来的时候，我们只需要去执行``Do()``的方法，要获取可能的错误，只需要使用``Error()``方法。而获取返回值，使用``Result()``方法的话，只能拿到一个``interface{}``类型的返回值，这并不是我们想要的，所以需要一个能够获取到具体类型返回值的函数：

	```go
	func Result(job Job, result interface{}) {
		t := reflect.TypeOf(result)
		if t.Kind() == reflect.Ptr {
			reflect.ValueOf(result).Elem().Set(reflect.ValueOf(job.Result()))
		}
	}
	```

	上面这个函数的用途在于，只要将某个 ``Job`` 以及这个 **``Job``返回值类型的零值指针``result``** 放进去，就能够将返回值正确赋值给``result``，类似 *``json.Unmarshal(data []byte,v interface{})``* 的用法。

	当外部某个具体的结构实现了上面 ``Job`` 接口的三个方法时，它的实例就能够被当做一个 ``Job`` 传递到线程池中。当一个 ``Job`` 执行完毕之后，接下来我们 *可能需要* 去处理一下执行完毕之后得到的返回值和错误，于是，下面又提供了另外两个接口：

	```go
	type ResultHandler interface{
		HandleResult()
	}

	type ErrorHandler interface{
		HandleError()
	}
	```

	用法大概会是这样的：如果我们想在程序执行完毕之后来处理返回值，那么我们就给刚刚实现了 ``Job`` 接口功能的结构，再实现上面 ``ResultHandler`` 接口的功能；如果是想处理错误，那么就实现 ``ErrorHandler`` 的功能；两个都实现的话，说明想同时处理错误和返回值。

现在，整个 ``ThreadPool`` 的结构已经明了了，于是我们有了一个重构之后的 ``NewThreadPool`` 函数：

```go
func NewThreadPool(volume int) *ThreadPool {
	return newThreadPool().init(volume)
}
```

因为刚刚上面对 ``worker`` 和 ``Job`` 已经定义完成，接下来应该处理这两个结构相关的问题。

在定义完了 ``worker`` 之后，我们知道 ``worker`` 其实只需要一个 ``id`` 来标识身份就够了；同时 ``ThreadPool`` 需要在 ``workerQueue`` 中取到闲置线程。所以，一个创建 ``worker`` 的方法就出现了：

```go
func newWorker(id int) *worker {
	w := new(worker)
	w.id = id
	return w
}
func (t *ThreadPool) createWorkers() {
	for i := 0; i < t.volume; i++ {
		t.workerQueue <- newWorker(i)
	}
}
```

当然也有用``gorutine``创建的方法，这样创建出来的 ``worker`` 是无序的：

```go
func (t *ThreadPool) createWorkers() *ThreadPool {
	var group sync.WaitGroup
	for i := 0; i < t.volume; i++ {
		group.Add(1)
		go func(i int) {
			t.workerQueue <- newWorker(i)
			group.Done()
		}(i)
	}
	group.Wait()
	return t
}
```

于是 ``NewThreadPool`` 方法更新成了：

```go
func NewThreadPool(volume int) *ThreadPool {
	return newThreadPool().init(volume).createWorkers()
}
```

当一个线程池里面所有线程都创建好了之后，接着便需要考虑如何处理线程池中收到的任务。目前有两种方法：
1. 一直监听 ``ThreadPool.jobs`` ，取到 ``Job`` 之后，立即从线程池中获取一个闲置线程来处理该任务；当终止线程池时，首先停止对 ``ThreadPool.jobs`` 的监听。
2. 一直从闲置线程队列 ``ThreadPool.workerQueue`` 中获取一个闲置的线程，取到一个闲置的线程之后，在这个线程上去监听 ``ThreadPool.jobs`` 和 ``ThreadPool.terminate`` ，如果有 ``Job`` 则处理 ``Job`` ，如果是 ``terminate`` 则终止

下面使用第2种逻辑来处理任务。首先是获取闲置的线程：

```go
func (t *ThreadPool) deliveryJobs() {
	for w := range t.workerQueue {
		// do something
	}
}
```

然后在一个线程上来处理任务或者终止信号：

```go
func (w *worker) work(t *ThreadPool) {
	select {
		case <- t.terminate:

		case job := <- t.jobs:

	}
}
```

更新 ``deliveryJobs()`` :

```go
func (t *ThreadPool) deliveryJobs() {
	for w := range t.workerQueue {
		w.work(t)
	}
}
```

于是 ``NewThreadPool(int)`` 函数继续被更新：

```go
func NewThreadPool(volume int) *ThreadPool {
	t := newThreadPool().init(volume).createWorkers()
	go t.deliveryJobs()
	return t
}
```

接下来是处理 ``(*worker) work()`` 方法。首先是 ``select`` 中的 ``<- terminate`` ``case``，此时我们得到要关闭整个线程池的信号，所以首先我们得将已经拿到的闲置线程重新放回队列，不让此线程去处理任何任务；然后将闲置线程中的所有线程从队列中一一取出，并不再放进去，直到取出 ``ThreadPool.volume`` 个线程，才说明没有任何闲置线程可用，并且也没有任何线程正在处理任务，此时线程池也就完全关闭了，程序可以安全退出；对于正在处理任务的线程，此时不必去考虑。

所以，我们先得有一个取出线程的方法：

```go
	func (t *ThreadPool) terminateWorkers() {
		for i := 0; i < t.volume; i++ {
			<- t.workerQueue
		}
	}
```

然后将这个方法加以运用：
```go
...
case <- terminate:
	t.workerQueue <- w
	t.terminateWorkers()
...
```

最后，我们希望外界有办法知道我们何时成功的关闭了所有的线程，所以，在内部成功关闭了所有线程之后，我们对 ``ThreadPool.terminate`` 从内部传递一个信号，用以外部接收：

```go
...
case <- t.terminate:
	t.workerQueue <- w
	t.terminateWorkers()
	t.terminate <- struct{}{}
...
```

更新代码：

```go
func (w *worker) work(t *ThreadPool) {
	select {
		case <- t.terminate:
			t.workerQueue <- w
			t.terminateWorkers()
			t.terminate <- struct{}{}
		case job := <- t.jobs:
			...
	}
}
```

接着是任务处理部分。当一个线程监听到的信号不是 ``terminate`` 而是 ``Job``，那么此时直接调用前面所说的方法来执行就好；同时，我们还要尝试调用处理返回值和错误的方法来对可能出现的返回值和错误进行处理；最后，任务完成，我们将线程重新放回闲置线程队列中，这样，如果是线程池正在关闭，那么它一定能在这个线程完成任务之后，在 ``workerQueue`` 中找到它，如果是持续运行的线程池，在一段时间之后，这个线程又会被捕获到，并重新给它分配新的任务：

```go
...
case job := <- t.jobs:
	job.Do()
	if v, ok := job.(ErrorHandler); ok {
		v.HandleError()
	}
	if v, ok := job.(ResultHandler); ok {
		v.HandleResult()
	}
	t.workerQueue <- w
...
```

更新 ``(*worker) work(*Terminate)``：

```go
func (w *worker) work(t *ThreadPool) {
	select {
		case <- t.terminate:
			t.workerQueue <- w
			t.terminateWorkers()
			t.terminate <- struct{}{}
		case job := <- t.jobs:
			job.Do()
			if v, ok := job.(ErrorHandler); ok {
				v.HandleError()
			}
			if v, ok := job.(ResultHandler); ok {
				v.HandleResult()
			}
			t.workerQueue <- w
	}
}
```

关于线程池对线程及任务的处理逻辑就已经全部完成，接下来是线程池的关闭还剩下的一点点事情：线程池关闭的信号的来源。我们应该有一个内部的方法来给线程池传递关闭信号，并且能知道线程池已经完全关闭：

```go
func (t *ThreadPool) close() {
	t.terminate <- struct{}{}
	<- t.terminate
}
```

同时外部也需要这个方法来对线程池进行关闭，包装一下就好：

```go
func (t *ThreadPool) Close {
	t.close()
}
```

同时我们应该需要在 ``NewThreadPool`` 的时候就能监测系统的关闭信号``Interrupt``：

```go
func (t *ThreadPool) waitForInterruptSignal() {
	signal.Notify(t.sysSignal, os.Interrupt)
	for {
		select {
		case <-t.sysSignal:
			t.close()
			os.Exit(0)
		}
	}
}
```

更新 ``NewThreadPool(int)`` :

```go
func NewThreadPool(volume int) *ThreadPool {
	t := newThreadPool().init(volume).createWorkers()
	go t.deliveryJobs()
	go t.waitForInterruptSignal()
	return t
}
```

至此，线程池对于线程、任务以及自身的关闭部分就已经全部处理完毕了。现在只剩下最后一件事情：任务的接收。

一个任务只需要在实现了 ``Job`` 的全部方法之后，就可以被传递到 ``ThreadPool`` 的任务接收 ``chan`` 中：

```go
func (t *ThreadPool) Execute(job *Job) {
	t.jobs <- job
}
```

最后，外部调用的情况应该是这样：

```go
type expectedJob struct {
	...
}

func (e *expectedJob) Do() { ... }
func (e *expectedJob) Result() { ... }
func (e *expectedJob) Error() { ... }

func (e *expectedJob) HandleResult() { ... }
func (e *expectedJob) HandleError() { ... }

func newJob(conditions ...interface{}) Job {
	return &expectedJob{ ... }
}

func main() {
	threadPoolVolume := 10
	threadPool := NewThreadPool(threadPoolVolume)
	defer threadPool.Close()
	
	for ... {
		job := newJob( ... )
		threadPool.Execute(job)
	}
}
```

---

### rust 实现线程池

### 线程池

``ThreadPool``结构中拥有以下元素：
- ``workers``一个向量，用来放置闲置的线程
- ``sender``用来发送待处理的事件

```rust
pub struct ThreadPool {
	workers: Vec<Worker>,
	sender: mpsc::Sender<Message>,
}
```

### 线程

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

### 事件处理逻辑

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

### 关闭线程池

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