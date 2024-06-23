package leakybucket

// A BucketsHeap implements heap.Interface and holds Items.
type BucketsHeap[TKey comparable] []LeakyBucket[TKey]

func (pq BucketsHeap[TKey]) Len() int { return len(pq) }

func (pq BucketsHeap[TKey]) Less(i, j int) bool {
	// we want for the oldest items to be the last in the list (as removing from the end of array is faster)
	return pq[i].LastAccessTime().After(pq[j].LastAccessTime())
}

func (pq BucketsHeap[TKey]) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].SetIndex(i)
	pq[j].SetIndex(j)
}

func (pq *BucketsHeap[TKey]) Push(x any) {
	n := len(*pq)
	item := x.(LeakyBucket[TKey])
	item.SetIndex(n)
	*pq = append(*pq, item)
}

func (pq *BucketsHeap[TKey]) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil    // avoid memory leak
	item.SetIndex(-1) // for safety
	*pq = old[0 : n-1]
	return item
}

func (pq *BucketsHeap[TKey]) Last() LeakyBucket[TKey] {
	count := len(*pq)
	if count > 0 {
		return (*pq)[count-1]
	}

	return nil
}
