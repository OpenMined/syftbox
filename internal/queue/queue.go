package queue

import (
	"container/heap"
	"sync"
)

// Item is a single item in the priority queue
type Item[T any] struct {
	Value    T
	Priority int
	index    int
}

// priorityQueueHeap implements heap.Interface
type priorityQueueHeap[T any] []*Item[T]

// Len returns the length of the priority queue
func (pqh priorityQueueHeap[T]) Len() int {
	return len(pqh)
}

// Less compares two items in the priority queue
// Priority values determine order: lower values = higher priority
func (pqh priorityQueueHeap[T]) Less(i, j int) bool {
	return pqh[i].Priority < pqh[j].Priority
}

// Swap swaps two items in the priority queue
func (pqh priorityQueueHeap[T]) Swap(i, j int) {
	pqh[i], pqh[j] = pqh[j], pqh[i]
	pqh[i].index = i
	pqh[j].index = j
}

// Push adds an item to the priority queue
func (pqh *priorityQueueHeap[T]) Push(x interface{}) {
	n := len(*pqh)
	item := x.(*Item[T])
	item.index = n
	*pqh = append(*pqh, item)
}

// Pop removes and returns the highest priority item from the priority queue
func (pqh *priorityQueueHeap[T]) Pop() interface{} {
	old := *pqh
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	*pqh = old[0 : n-1]
	return item
}

// PriorityQueue implements a thread-safe generic priority queue
type PriorityQueue[T any] struct {
	heap priorityQueueHeap[T]
	mu   sync.Mutex
}

// NewPriorityQueue creates a new priority queue
func NewPriorityQueue[T any]() *PriorityQueue[T] {
	pq := &PriorityQueue[T]{
		heap: make(priorityQueueHeap[T], 0),
	}
	heap.Init(&pq.heap)
	return pq
}

// Len returns the length of the priority queue
func (pq *PriorityQueue[T]) Len() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.heap.Len()
}

// Enqueue adds a value to the priority queue with the given priority
func (pq *PriorityQueue[T]) Enqueue(value T, priority int) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	item := &Item[T]{
		Value:    value,
		Priority: priority,
	}
	heap.Push(&pq.heap, item)
}

// Dequeue removes and returns the highest priority item from the queue
func (pq *PriorityQueue[T]) Dequeue() (T, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if pq.heap.Len() == 0 {
		var zero T
		return zero, false
	}

	item := heap.Pop(&pq.heap).(*Item[T])
	return item.Value, true
}

func (pq *PriorityQueue[T]) DequeueAll() []T {
	items := make([]T, 0, pq.Len())
	for pq.Len() > 0 {
		item, _ := pq.Dequeue()
		items = append(items, item)
	}
	return items
}
