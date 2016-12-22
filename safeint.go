package smpperf

import (
	"sync"
)

type SafeInt struct {
	val int
	m   *sync.RWMutex
}

func NewSafeInt(n int) *SafeInt {
	return &SafeInt{val: n, m: &sync.RWMutex{}}
}

func (s *SafeInt) Increment() {
	s.m.Lock()
	defer s.m.Unlock()
	s.val += 1
}

func (s *SafeInt) Val() int {
	s.m.RLock()
	defer s.m.RUnlock()
	return s.val
}
