package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ppiankov/clickspectre/internal/models"
)

// WorkerPool manages concurrent processing of query log entries
type WorkerPool struct {
	workers int
	jobs    chan *models.QueryLogEntry
	results chan *models.QueryLogEntry
	errors  chan error
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	mu      sync.Mutex
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workers int) *WorkerPool {
	return &WorkerPool{
		workers: workers,
		jobs:    make(chan *models.QueryLogEntry, workers*2),
		results: make(chan *models.QueryLogEntry, workers*2),
		errors:  make(chan error, workers),
	}
}

// Start starts the worker pool
func (p *WorkerPool) Start(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.started = true

	// Start workers
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// worker processes jobs from the job queue
func (p *WorkerPool) worker(id int) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("worker panic recovered",
				slog.Int("worker_id", id),
				slog.String("panic", fmt.Sprint(r)),
			)
		}
		p.wg.Done()
	}()

	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobs:
			if !ok {
				return
			}

			// Process the job
			// In this case, we're just passing through
			// but this is where you could do additional processing
			p.results <- job
		}
	}
}

// Submit submits a job to the worker pool
func (p *WorkerPool) Submit(entry *models.QueryLogEntry) {
	select {
	case <-p.ctx.Done():
		return
	case p.jobs <- entry:
	}
}

// Results returns the results channel
func (p *WorkerPool) Results() <-chan *models.QueryLogEntry {
	return p.results
}

// Errors returns the errors channel
func (p *WorkerPool) Errors() <-chan error {
	return p.errors
}

// Stop stops the worker pool and waits for all workers to finish
func (p *WorkerPool) Stop() {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	// Close jobs channel to signal workers to stop
	close(p.jobs)

	// Wait for all workers to finish
	p.wg.Wait()

	// Close results and errors channels
	close(p.results)
	close(p.errors)

	// Cancel context
	if p.cancel != nil {
		p.cancel()
	}

	p.mu.Lock()
	p.started = false
	p.mu.Unlock()
}
