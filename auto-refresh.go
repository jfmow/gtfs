package gtfs

import (
	"fmt"

	"github.com/robfig/cron"
)

func (v Database) EnableAutoUpdate() {
	c := cron.New()

	// Add the cron schedule and task to the cron job
	c.AddFunc("@every 00h30m00s", func() {
		isValid, err := v.IsFeedDataUpToDate()
		if err != nil {
			fmt.Println(err)
		}
		if !isValid {
			fmt.Println("Feed data is outdated, updating...")

			// Delete old data if it's outdated
			err := v.deleteOldData()
			if err != nil {
				fmt.Printf("Failed to delete old data: %v", err)
			}

			// Fetch and write new data
			data, err := fetchZip(v.url)
			if err != nil {
				fmt.Printf("Failed to fetch new data: %v", err)
			}
			err = writeFilesToDB(data, v.db)
			if err != nil {
				fmt.Printf("Failed to write new data to the database: %v", err)
			}
			fmt.Println("Feed data updated automatically")
		}
	})

	// Start the cron job scheduler
	c.Start()
}
