package main

import "context"

// Semaphore limits concurrent in-flight requests to protect the worker.
type Semaphore struct {
	ch chan struct{}
}

func NewSemaphore(max int) *Semaphore {
	if max <= 0 {
		max = 1
	}
	return &Semaphore{ch: make(chan struct{}, max)}
}

func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Semaphore) Release() {
	<-s.ch
	// select {
	// case <-s.ch:
	// default:
	// }
}
