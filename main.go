package gtfs

import (
	"fmt"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type Database struct {
	db              *sqlx.DB
	url             string
	timeZone        *time.Location
	mailToEmail     string
	apiKey          ApiKey
	name            string
	refreshNotifier *notifier
}

/*
# Creates a new gtfs instance

  - url: url to gtfs .zip (e.g., "https://example.com/gtfs.zip")

  - databaseName: the name for the .db file to be created with (e.g., "transit_data.db")

  - tz: the timezone to process gtfs with (e.g., time.UTC)

  - mailToEmail: the email to use with notifications (e.g., "hi@example.com" (NOT: "mailto:hi@example.com"))

  - **apiKey**: --optional field--, only required if the gtfs.zip file requires an API key in the request (e.g., "your-api-key").
*/
func New(url string, apiKey ApiKey, databaseName string, tz *time.Location, mailToEmail string) (Database, error) {
	database, err := newDatabase(url, apiKey, databaseName, tz, mailToEmail)
	if err != nil {
		panic(err)
	}

	// Check if the feed data is still up to date
	isUpToDate, err := database.IsFeedDataUpToDate()

	if !isUpToDate || err != nil {
		fmt.Println("Feed data is not up to date: " + databaseName)
		if err := database.refreshWithRetries(3, 30*time.Second); err != nil {
			log.Printf("gtfs: initial refresh failed for %s: %v", databaseName, err)
		}
	} else {
		fmt.Println("Feed data is still up to date: " + databaseName)
		if err := database.createIndexesTx(); err != nil {
			log.Printf("gtfs: failed to create indexes for %s: %v", databaseName, err)
		}
	}

	database.EnableAutoUpdateGTFSData()

	return database, nil
}

func (v Database) IsFeedDataUpToDate() (bool, error) {
	// Parse the feed_end_date to a time.Time object
	feedEndTime, err := v.FeedEndDate()
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

func (v Database) LocalTimeZone() *time.Location {
	return v.timeZone
}
