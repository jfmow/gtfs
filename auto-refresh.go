package gtfs

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

var cronMutex sync.Mutex

// refreshWithRetries attempts to refresh the database data, retrying with a
// fixed delay between attempts on failure, instead of leaving the app to wait
// for the next scheduled refresh (up to 24h later) after a single transient
// failure (e.g. a flaky upstream feed fetch).
func (v Database) refreshWithRetries(attempts int, delay time.Duration) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = v.refreshDatabaseData(); err == nil {
			return nil
		}
		log.Printf("gtfs refresh attempt %d/%d failed for %s: %v", i+1, attempts, v.name, err)
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}
	return err
}

func (v Database) EnableAutoUpdateGTFSData() {
	c := cron.New(cron.WithLocation(v.timeZone))

	// Run at 1 AM every day
	c.AddFunc("0 1 * * *", func() {
		cronMutex.Lock()
		defer cronMutex.Unlock()
		fmt.Println("Refreshing database data... (1 AM)")
		if err := v.refreshWithRetries(3, 5*time.Minute); err != nil {
			fmt.Printf("Failed to refresh %s gtfs database: %v\n", v.name, err)
		}
	})

	// Start the cron job scheduler
	c.Start()
}
