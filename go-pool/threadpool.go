package pool

import (
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"sync"
)

func (t *ThreadPool) terminateWorkers() {
	for i := 0; i < t.volume; i++ {
		fmt.Printf("%d workers remains to be terminated\n", t.volume-i)
		w := <-t.workerQueue
		fmt.Printf("terminate worker %d\n", w.id)
	}
	fmt.Printf("terminate workers finished.\n")
}

func (w *worker) work(t *ThreadPool) {
	select {
	case <-t.terminate:
		t.workerQueue <- w
		t.terminateWorkers()
		t.terminate <- struct{}{}
		return
	case job := <-t.jobs:
		fmt.Printf("worker %d get a job: %v\n", w.id, job)
		go func(t *ThreadPool, w *worker) {
			job.Do()
			if job.Error() != nil {
				fmt.Printf("job got an error: %v\n", job.Error())
				if v, ok := job.(ErrorHandler); ok {
					fmt.Printf("handle error\n")
					v.HandleError()
				}
			}
			if v, ok := job.(ResultHandler); ok {
				fmt.Printf("handle result\n")
				v.HandleResult()
			}
			fmt.Printf("worker %d has finished the job\n", w.id)
			t.workerQueue <- w
			fmt.Printf("worker %d is waiting for a job\n", w.id)
		}(t, w)
	}
}

func (t *ThreadPool) deliveryJobs() {
	fmt.Printf("start to delivery jobs\n")
	for w := range t.workerQueue {
		w.work(t)
	}
}

func (t *ThreadPool) newWorker(id int) {
	w := new(worker)
	w.id = id
	t.workerQueue <- w
	fmt.Printf("create worker %d\n", w.id)
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

func newThreadPool() *ThreadPool {
	return new(ThreadPool)
}

func (t *ThreadPool) init(volume int) *ThreadPool {
	t.jobs = make(chan Job)
	t.terminate = make(chan struct{})
	t.sysSignal = make(chan os.Signal)
	t.workerQueue = make(chan *worker, volume)
	t.volume = volume

	return t
}

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

func NewThreadPool(volume int) *ThreadPool {
	if volume <= 0 {
		panic("invalid go rutine number")
	}
	t := newThreadPool().init(volume).createWorkers()
	go t.deliveryJobs()
	go t.close()
	return t
}

func (t *ThreadPool) Execute(job Job) {
	fmt.Printf("got a job: %v\n", job)
	t.jobs <- job
}

func (t *ThreadPool) Close() {
	fmt.Printf("start to close thread pool\n")
	t.terminate <- struct{}{}
	// if len(t.jobsDone) == 0 {
	<-t.terminate
	// }
	fmt.Printf("close thread pool finished\n")
}

func Result(job Job, result interface{}) {
	t := reflect.TypeOf(result)
	if t.Kind() == reflect.Ptr {
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(job.Result()))
	}
}
