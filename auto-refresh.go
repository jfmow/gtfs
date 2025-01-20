package gtfs

import (
	"github.com/robfig/cron"
)

func (v Database) EnableAutoUpdateGTFSData() {
	c := cron.New()

	c.AddFunc("@every 01h00m00s", func() {
		v.refreshDatabaseData()
	})

	// Start the cron job scheduler
	c.Start()
}
