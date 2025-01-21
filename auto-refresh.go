package gtfs

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

func (v Database) EnableAutoUpdateGTFSData() {
	c := cron.New(cron.WithLocation(time.FixedZone("NZST", 13*60*60)))

	// Run at 11 PM every day
	c.AddFunc("0 23 * * *", func() {
		fmt.Println("Refreshing database data... (11 PM)")
		v.refreshDatabaseData()
	})

	// Run at 3 AM every day
	c.AddFunc("0 3 * * *", func() {
		fmt.Println("Refreshing database data... (3 AM)")
		v.refreshDatabaseData()
	})

	// Start the cron job scheduler
	c.Start()
}
