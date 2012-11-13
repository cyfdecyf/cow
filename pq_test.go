package main

import (
	"container/heap"
	"testing"
)

func TestRequestPQ(t *testing.T) {
	reqArr := []*Request{
		&Request{no: 2},
		&Request{no: 4},
		&Request{no: 3},
		&Request{no: 5},
		&Request{no: 1},
	}

	pq := make(RequestPQ, 0, 2)

	for _, v := range reqArr {
		heap.Push(&pq, v)
	}

	for i := 1; i <= 5; i++ {
		item := heap.Pop(&pq)
		req, _ := item.(*Request)
		if req.no != i {
			t.Errorf("req no %d, should be %d\n", req.no, i)
		}
	}
}
