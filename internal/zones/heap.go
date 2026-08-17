package zones

import (
	"sort"
	"sync"
	"github.com/geodispatch/supervisor/internal/models"
)

type MinHeap struct {
	mu       sync.Mutex
	devices  []models.TriagedDevice
	ch       chan models.TriagedDevice
	isClosed bool
}

func NewMinHeap() *MinHeap {
	return &MinHeap{ch: make(chan models.TriagedDevice, 100)}
}

func (h *MinHeap) Push(d models.TriagedDevice) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.devices = append(h.devices, d)
	if !h.isClosed {
		h.ch <- d
	}
}

func (h *MinHeap) Stream() <-chan models.TriagedDevice {
	return h.ch
}

func (h *MinHeap) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.devices)
}

func (h *MinHeap) PopN(n int) []models.TriagedDevice {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	// Sort by distance (ascending)
	sort.Slice(h.devices, func(i, j int) bool {
		return h.devices[i].DistanceKm < h.devices[j].DistanceKm
	})

	if n > len(h.devices) {
		n = len(h.devices)
	}
	batch := h.devices[:n]
	h.devices = h.devices[n:]
	return batch
}

func (h *MinHeap) PopAll() []models.TriagedDevice {
	return h.PopN(h.Len())
}

func (h *MinHeap) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isClosed = true
	close(h.ch)
}