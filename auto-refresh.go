package gtfs

import (
	"github.com/robfig/cron"
)

func (v Database) EnableAutoUpdateGTFSData() {
	c := cron.New()

	// Run at 11 PM every day
	c.AddFunc("0 23 * * *", func() {
		v.refreshDatabaseData()
	})

	// Run at 3 AM every day
	c.AddFunc("0 3 * * *", func() {
		v.refreshDatabaseData()
	})

	// Start the cron job scheduler
	c.Start()
}
