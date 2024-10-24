package gtfs

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Database struct {
	db  *sql.DB
	url string
}

func (v Database) IsFeedDataUpToDate() (bool, error) {
	var feedEndDate string

	// Query to get the feed_end_date from the feed_info table
	query := "SELECT feed_end_date FROM feed_info LIMIT 1"
	err := v.db.QueryRow(query).Scan(&feedEndDate)
	if err != nil {
		return false, fmt.Errorf("failed to query feed_info: %w", err)
	}

	// Parse the feed_end_date to a time.Time object
	feedEndTime, err := time.Parse("20060102", feedEndDate)
	if err != nil {
		return false, fmt.Errorf("failed to parse feed_end_date: %w", err)
	}

	// Compare feed_end_date with the current date
	currentTime := time.Now()
	if feedEndTime.After(currentTime) {
		return true, nil // Data is still valid
	}

	return false, nil // Data is outdated
}
func (v Database) FeedEndDate() (time.Time, error) {
	var feedEndDate string

	// Query to get the feed_end_date from the feed_info table
	query := "SELECT feed_end_date FROM feed_info LIMIT 1"
	err := v.db.QueryRow(query).Scan(&feedEndDate)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to query feed_info: %w", err)
	}

	// Parse the feed_end_date to a time.Time object
	feedEndTime, err := time.Parse("20060102", feedEndDate)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse feed_end_date: %w", err)
	}

	return feedEndTime, nil
}

// Delete old data from the database
func (v Database) deleteOldData() error {
	// Delete data from relevant tables (customize as needed)
	tables := []string{"stop_times", "trips", "stops", "calendar_dates", "feed_info"}
	for _, table := range tables {
		query := fmt.Sprintf("DELETE FROM %s", table)
		_, err := v.db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to delete data from table %s: %w", table, err)
		}
	}
	fmt.Println("Old data deleted successfully")
	return nil
}

// New creates a new database instance and checks for feed updates
func New(url string, databaseName string) (Database, error) {
	if url == "" {
		return Database{}, errors.New("missing url")
	}
	if len(databaseName) < 3 {
		return Database{}, errors.New("database name to short >3")
	}

	os.Mkdir(filepath.Join(GetWorkDir(), "gtfs"), os.ModePerm)

	db, err := sql.Open("sqlite", filepath.Join(GetWorkDir(), "gtfs", fmt.Sprintf("gtfs-%s.db", databaseName)))
	if err != nil {
		log.Fatal(err)
	}

	// Enable WAL mode
	_, err = db.Exec("PRAGMA journal_mode = WAL;")
	if err != nil {
		log.Fatalf("Failed to set WAL mode: %v", err)
	}
	fmt.Println("WAL mode enabled")

	// Initialize the Database struct
	database := Database{db: db, url: url}

	// Check if the feed data is still up to date
	isUpToDate, err := database.IsFeedDataUpToDate()

	if !isUpToDate || err != nil {
		fmt.Println("Feed data is outdated, updating...")

		// Delete old data if it's outdated
		if err == nil {
			err := database.deleteOldData()
			if err != nil {
				log.Fatalf("Failed to delete old data: %v", err)
			}
		}

		// Fetch and write new data
		data, err := fetchZip(url)
		if err != nil {
			log.Fatalf("Failed to fetch new data: %v", err)
		}
		err = writeFilesToDB(data, db)
		if err != nil {
			log.Fatalf("Failed to write new data to the database: %v", err)
		}

		fmt.Println("Data updated successfully.")

	} else {
		fmt.Println("Feed data is still up to date.")
	}

	_, err = database.precomputeServices()
	if err != nil {
		panic(err)
	}

	database.EnableAutoUpdate()

	return database, nil
}
