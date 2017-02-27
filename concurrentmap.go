package smpperf

import (
	"sync"
)

type ConcurrentMap struct {
	mp    map[string]string
	mutex sync.RWMutex
}

func NewConcurrentMap() *ConcurrentMap {
	return &ConcurrentMap{
		mp: map[string]string{},
	}
}

func (m *ConcurrentMap) Set(key string, value string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.mp[key] = value
}

func (m *ConcurrentMap) Get(key string) (string, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	v, ok := m.mp[key]
	return v, ok
}

func (m *ConcurrentMap) Delete(key string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.mp, key)
}

func (m *ConcurrentMap) Keys() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	keys := make([]string, len(m.mp))
	i := 0
	for key := range m.mp {
		keys[i] = key
		i++
	}
	return keys
}
