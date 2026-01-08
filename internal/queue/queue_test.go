package queue

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPriorityQueue_OrdersByPriority(t *testing.T) {
	pq := NewPriorityQueue[string]()
	pq.Enqueue("low", 10)
	pq.Enqueue("high", 1)
	pq.Enqueue("mid", 5)

	v, ok := pq.Dequeue()
	assert.True(t, ok)
	assert.Equal(t, "high", v)

	v, ok = pq.Dequeue()
	assert.True(t, ok)
	assert.Equal(t, "mid", v)

	v, ok = pq.Dequeue()
	assert.True(t, ok)
	assert.Equal(t, "low", v)

	_, ok = pq.Dequeue()
	assert.False(t, ok)
}

func TestPriorityQueue_DequeueAll(t *testing.T) {
	pq := NewPriorityQueue[int]()
	pq.Enqueue(1, 3)
	pq.Enqueue(2, 2)
	pq.Enqueue(3, 1)
	assert.Equal(t, 3, pq.Len())

	all := pq.DequeueAll()
	assert.Equal(t, []int{3, 2, 1}, all)
	assert.Equal(t, 0, pq.Len())
}

func TestPriorityQueue_ConcurrentEnqueue(t *testing.T) {
	pq := NewPriorityQueue[int]()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			pq.Enqueue(v, v)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 50, pq.Len())
}

