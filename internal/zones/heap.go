package zones

import (
	"sort"
	"sync"

	"github.com/geodispatch/supervisor/internal/models"
)

type MinHeap struct {
	mu       sync.Mutex
	devices  []models.TriagedDevice
	ready    chan struct{}
	isClosed bool
}

func NewMinHeap() *MinHeap {
	return &MinHeap{
		// buffer = max phones you'd ever process in one event
		ready: make(chan struct{}, 200),
	}
}

func (h *MinHeap) Push(d models.TriagedDevice) {
	h.mu.Lock()
	h.devices = append(h.devices, d)
	closed := h.isClosed
	h.mu.Unlock()

	if !closed {
		// non-blocking signal — drainer wakes up and checks Len()
		select {
		case h.ready <- struct{}{}:
		default:
		}
	}
}

// Stream returns a channel that signals whenever new devices are available.
func (h *MinHeap) Stream() <-chan struct{} {
	return h.ready
}

func (h *MinHeap) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.devices)
}

// PopN sorts by distance then pops the first n devices.
func (h *MinHeap) PopN(n int) []models.TriagedDevice {
	h.mu.Lock()
	defer h.mu.Unlock()

	sort.Slice(h.devices, func(i, j int) bool {
		return h.devices[i].DistanceKm < h.devices[j].DistanceKm
	})

	if n > len(h.devices) {
		n = len(h.devices)
	}

	batch := make([]models.TriagedDevice, n)
	copy(batch, h.devices[:n])
	h.devices = h.devices[n:]
	return batch
}

func (h *MinHeap) PopAll() []models.TriagedDevice {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.devices) == 0 {
		return nil
	}

	sort.Slice(h.devices, func(i, j int) bool {
		return h.devices[i].DistanceKm < h.devices[j].DistanceKm
	})

	batch := make([]models.TriagedDevice, len(h.devices))
	copy(batch, h.devices)
	h.devices = nil
	return batch
}

func (h *MinHeap) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isClosed = true
	close(h.ready)
}