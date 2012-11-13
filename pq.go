package main

type RequestPQ []*Request

func (pq RequestPQ) Len() int { return len(pq) }

func (pq RequestPQ) Less(i, j int) bool {
	// Pop return the request with the lowest no, so use less than here
	return pq[i].no < pq[j].no
}

func (pq RequestPQ) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *RequestPQ) Push(x interface{}) {
	*pq = append(*pq, x.(*Request))
}

func (pq *RequestPQ) Pop() interface{} {
	a := *pq
	n := len(a)
	item := a[n-1]
	*pq = a[0 : n-1]
	return item
}
