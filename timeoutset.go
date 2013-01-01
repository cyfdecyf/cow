package main

import (
	"sync"
	"time"
)

type TimeoutSet struct {
	sync.RWMutex
	time    map[string]time.Time
	timeout time.Duration
}

func NewTimeoutSet(timeout time.Duration) *TimeoutSet {
	ts := &TimeoutSet{time: make(map[string]time.Time),
		timeout: timeout,
	}
	return ts
}

func (ts *TimeoutSet) add(key string) {
	now := time.Now()
	ts.Lock()
	ts.time[key] = now
	ts.Unlock()
}

func (ts *TimeoutSet) has(key string) bool {
	ts.RLock()
	t, ok := ts.time[key]
	ts.RUnlock()
	if !ok {
		return false
	}
	if time.Now().Sub(t) > ts.timeout {
		ts.del(key)
		return false
	}
	return true
}

func (ts *TimeoutSet) del(key string) {
	ts.Lock()
	delete(ts.time, key)
	ts.Unlock()
}
