package gtfs

import (
	"sync"
	"time"
)

// The `Identity` function in Go returns the input value along with a nil error.
// AKA the default value for generate a cache with no transform function
func Identity[T any](x T) (T, error) {
	return x, nil
}

// Creates a cached value that is periodically refreshed using a
// specified refresh function and interval.
func GenerateACache[In any, Out any](refreshFunc func() (In, error), transform func(In) (Out, error), refreshInterval time.Duration) (func() Out, error) {
	var (
		cache Out
		mu    sync.RWMutex
	)

	refreshCache := func() error {
		data, err := refreshFunc()
		if err != nil {
			return err
		}

		processed, err := transform(data)
		if err != nil {
			return err
		}

		mu.Lock()
		cache = processed
		mu.Unlock()
		return nil
	}

	if err := refreshCache(); err != nil {
		return nil, err
	}

	go func() {
		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()

		for range ticker.C {
			_ = refreshCache()
		}
	}()

	return func() Out {
		mu.RLock()
		defer mu.RUnlock()
		return cache
	}, nil
}
