package queue

import (
	"high_throughput_system/internal/ledger"
	"sync"
)

type WorkerPool struct {
	numWorkers int
	batcher    *ledger.Batcher
	jobQueue   chan ledger.Transaction
	wg         sync.WaitGroup
	quit       chan struct{}
}

func NewWorkerPool(numWorkers int, batcher *ledger.Batcher) *WorkerPool {
	return &WorkerPool{
		numWorkers: numWorkers,
		batcher:    batcher,
		jobQueue:   make(chan ledger.Transaction, 10000),
		quit:       make(chan struct{}),
	}
}

func (p *WorkerPool) Start() {
	for i := 0; i < p.numWorkers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

func (p *WorkerPool) Stop() {
	close(p.quit)
	p.wg.Wait()
}

func (p *WorkerPool) Submit(tx ledger.Transaction) {
	p.jobQueue <- tx
}

func (p *WorkerPool) worker() {
	defer p.wg.Done()
	for {
		select {
		case job := <-p.jobQueue:
			// Perform validation logic here (e.g., KYC check simulated)
			// Then pass to batcher for persistence
			p.batcher.Add(job)
		case <-p.quit:
			return
		}
	}
}
