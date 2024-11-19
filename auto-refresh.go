package gtfs

import (
	"fmt"

	"github.com/robfig/cron"
)

func (v Database) EnableAutoUpdateGTFSData() {
	c := cron.New()

	c.AddFunc("@every 01h00m00s", func() {
		updateData(v)
	})

	// Start the cron job scheduler
	c.Start()
}

func updateData(v Database) {
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
}
