package gtfs

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type Database struct {
	db  *sqlx.DB
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

// New creates a new database instance and checks for feed updates
func New(url string, databaseName string) (Database, error) {
	database, err := newDatabase(url, databaseName)
	if err != nil {
		panic(err)
	}

	// Check if the feed data is still up to date
	isUpToDate, err := database.IsFeedDataUpToDate()

	if !isUpToDate || err != nil {
		database.refreshDatabaseData()
	} else {
		fmt.Println("Feed data is still up to date.")
		database.createIndexes()
	}

	database.EnableAutoUpdateGTFSData()

	return database, nil
}
