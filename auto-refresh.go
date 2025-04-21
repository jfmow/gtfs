package gtfs

import (
	"fmt"
	"sync"

	"github.com/robfig/cron/v3"
)

var cronMutex sync.Mutex

func (v Database) EnableAutoUpdateGTFSData() {
	c := cron.New(cron.WithLocation(v.timeZone))

	// Run at 1 AM every day
	c.AddFunc("0 1 * * *", func() {
		cronMutex.Lock()
		defer cronMutex.Unlock()
		fmt.Println("Refreshing database data... (1 AM)")
		if err := v.refreshDatabaseData(); err != nil {
			fmt.Printf("Failed to refresh %s gtfs database", v.name)
		}
	})

	// Start the cron job scheduler
	c.Start()
}
