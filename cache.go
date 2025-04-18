package gtfs

import (
	"log"
	"sync"
)

// The `Identity` function in Go returns the input value along with a nil error.
// AKA the default value for generate a cache with no transform function
func Identity[T any](x T) (T, error) {
	return x, nil
}

// Creates a cached value that is refreshed when the database's RefreshNotifier channel is triggered.
func GenerateACache[In any, Out any](
	refreshFunc func() (In, error),
	transform func(In) (Out, error),
	emptyValue Out,
	v Database,
) (func() Out, error) {
	var (
		cache Out
		mu    sync.RWMutex
	)

	refreshCache := func() error {
		data, err := refreshFunc()
		if err != nil {
			cache = emptyValue
			log.Println(err)
			return nil
		}

		processed, err := transform(data)
		if err != nil {
			cache = emptyValue
			log.Println(err)
			return nil
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
		for range v.RefreshNotifier {
			_ = refreshCache()
		}
	}()

	return func() Out {
		mu.RLock()
		defer mu.RUnlock()
		return cache
	}, nil
}
