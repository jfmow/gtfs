package gtfs

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jmoiron/sqlx"
)

func newDatabase(url string, databaseName string) (Database, error) {
	if url == "" {
		return Database{}, errors.New("missing url")
	}
	if len(databaseName) < 3 {
		return Database{}, errors.New("database name to short >3")
	}

	os.Mkdir(filepath.Join(GetWorkDir(), "gtfs"), os.ModePerm)

	db, err := sqlx.Open("sqlite", filepath.Join(GetWorkDir(), "gtfs", fmt.Sprintf("gtfs-%s.db", databaseName)))
	if err != nil {
		fmt.Println(err)
		panic("Failed to open the database")
	}

	// Enable WAL mode
	_, err = db.Exec("PRAGMA journal_mode = WAL;")
	if err != nil {
		panic("Failed to set WAL mode")
	}

	// Initialize the Database struct
	database := Database{db: db, url: url}
	return database, nil
}

func (v Database) createDefaultGTFSTables() {
	query := `
		-- Table: agency
		CREATE TABLE IF NOT EXISTS agency (
			agency_id TEXT PRIMARY KEY,
			agency_name TEXT NOT NULL DEFAULT '',
			agency_url TEXT NOT NULL DEFAULT '',
			agency_timezone TEXT NOT NULL DEFAULT '',
			agency_lang TEXT DEFAULT '',
			agency_phone TEXT DEFAULT '',
			agency_fare_url TEXT DEFAULT '',
			agency_email TEXT DEFAULT ''
		);

		-- Table: stops
		CREATE TABLE IF NOT EXISTS stops (
			stop_id TEXT PRIMARY KEY,
			stop_code TEXT DEFAULT '',
			stop_name TEXT NOT NULL DEFAULT '',
			stop_desc TEXT DEFAULT '',
			stop_lat REAL NOT NULL DEFAULT 0.0,
			stop_lon REAL NOT NULL DEFAULT 0.0,
			zone_id TEXT DEFAULT '',
			stop_url TEXT DEFAULT '',
			location_type INTEGER DEFAULT 0,
			parent_station TEXT DEFAULT '',
			stop_timezone TEXT DEFAULT '',
			wheelchair_boarding INTEGER DEFAULT 0,
			level_id TEXT DEFAULT '',
			platform_code TEXT DEFAULT ''
		);

		-- Table: routes
		CREATE TABLE IF NOT EXISTS routes (
			route_id TEXT PRIMARY KEY,
			agency_id TEXT DEFAULT '',
			route_short_name TEXT NOT NULL DEFAULT '',
			route_long_name TEXT NOT NULL DEFAULT '',
			route_desc TEXT DEFAULT '',
			route_type INTEGER NOT NULL DEFAULT 0,
			route_url TEXT DEFAULT '',
			route_color TEXT DEFAULT '',
			route_text_color TEXT DEFAULT '',
			route_sort_order INTEGER DEFAULT 0,
			continuous_pickup INTEGER DEFAULT 0,
			continuous_drop_off INTEGER DEFAULT 0,
			FOREIGN KEY (agency_id) REFERENCES agency (agency_id)
		);

		-- Table: trips
		CREATE TABLE IF NOT EXISTS trips (
			trip_id TEXT PRIMARY KEY,
			route_id TEXT NOT NULL DEFAULT '',
			service_id TEXT NOT NULL DEFAULT '',
			trip_headsign TEXT DEFAULT '',
			trip_short_name TEXT DEFAULT '',
			direction_id INTEGER DEFAULT 0,
			block_id TEXT DEFAULT '',
			shape_id TEXT DEFAULT '',
			wheelchair_accessible INTEGER DEFAULT 0,
			bikes_allowed INTEGER DEFAULT 0,
			FOREIGN KEY (route_id) REFERENCES routes (route_id)
		);

		-- Table: stop_times
		CREATE TABLE IF NOT EXISTS stop_times (
			trip_id TEXT NOT NULL DEFAULT '',
			arrival_time TEXT DEFAULT '',
			departure_time TEXT DEFAULT '',
			stop_id TEXT NOT NULL DEFAULT '',
			stop_sequence INTEGER NOT NULL DEFAULT 0,
			stop_headsign TEXT DEFAULT '',
			pickup_type INTEGER DEFAULT 0,
			drop_off_type INTEGER DEFAULT 0,
			continuous_pickup INTEGER DEFAULT 0,
			continuous_drop_off INTEGER DEFAULT 0,
			shape_dist_traveled REAL DEFAULT 0.0,
			timepoint INTEGER DEFAULT 0,
			PRIMARY KEY (trip_id, stop_sequence),
			FOREIGN KEY (trip_id) REFERENCES trips (trip_id),
			FOREIGN KEY (stop_id) REFERENCES stops (stop_id)
		);

		-- Table: calendar
		CREATE TABLE IF NOT EXISTS calendar (
			service_id TEXT PRIMARY KEY,
			monday INTEGER NOT NULL DEFAULT 0,
			tuesday INTEGER NOT NULL DEFAULT 0,
			wednesday INTEGER NOT NULL DEFAULT 0,
			thursday INTEGER NOT NULL DEFAULT 0,
			friday INTEGER NOT NULL DEFAULT 0,
			saturday INTEGER NOT NULL DEFAULT 0,
			sunday INTEGER NOT NULL DEFAULT 0,
			start_date TEXT NOT NULL DEFAULT '',
			end_date TEXT NOT NULL DEFAULT ''
		);

		-- Table: calendar_dates
		CREATE TABLE IF NOT EXISTS calendar_dates (
			service_id TEXT NOT NULL DEFAULT '',
			date TEXT NOT NULL DEFAULT '',
			exception_type INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (service_id, date),
			FOREIGN KEY (service_id) REFERENCES calendar (service_id)
		);

		-- Table: fare_attributes
		CREATE TABLE IF NOT EXISTS fare_attributes (
			fare_id TEXT PRIMARY KEY,
			price REAL NOT NULL DEFAULT 0.0,
			currency_type TEXT NOT NULL DEFAULT '',
			payment_method INTEGER NOT NULL DEFAULT 0,
			transfers INTEGER DEFAULT 0,
			agency_id TEXT DEFAULT '',
			transfer_duration INTEGER DEFAULT 0,
			FOREIGN KEY (agency_id) REFERENCES agency (agency_id)
		);

		-- Table: fare_rules
		CREATE TABLE IF NOT EXISTS fare_rules (
			fare_id TEXT NOT NULL DEFAULT '',
			route_id TEXT DEFAULT '',
			origin_id TEXT DEFAULT '',
			destination_id TEXT DEFAULT '',
			contains_id TEXT DEFAULT '',
			FOREIGN KEY (fare_id) REFERENCES fare_attributes (fare_id),
			FOREIGN KEY (route_id) REFERENCES routes (route_id)
		);

		-- Table: shapes
		CREATE TABLE IF NOT EXISTS shapes (
			shape_id TEXT NOT NULL DEFAULT '',
			shape_pt_lat REAL NOT NULL DEFAULT 0.0,
			shape_pt_lon REAL NOT NULL DEFAULT 0.0,
			shape_pt_sequence INTEGER NOT NULL DEFAULT 0,
			shape_dist_traveled REAL DEFAULT 0.0,
			PRIMARY KEY (shape_id, shape_pt_sequence)
		);

		-- Table: frequencies
		CREATE TABLE IF NOT EXISTS frequencies (
			trip_id TEXT NOT NULL DEFAULT '',
			start_time TEXT NOT NULL DEFAULT '',
			end_time TEXT NOT NULL DEFAULT '',
			headway_secs INTEGER NOT NULL DEFAULT 0,
			exact_times INTEGER DEFAULT 0,
			FOREIGN KEY (trip_id) REFERENCES trips (trip_id)
		);

		-- Table: transfers
		CREATE TABLE IF NOT EXISTS transfers (
			from_stop_id TEXT NOT NULL DEFAULT '',
			to_stop_id TEXT NOT NULL DEFAULT '',
			from_trip_id TEXT DEFAULT '',
			to_trip_id TEXT DEFAULT '',
			transfer_type INTEGER NOT NULL DEFAULT 0,
			min_transfer_time INTEGER DEFAULT 0,
			PRIMARY KEY (from_stop_id, to_stop_id, from_trip_id, to_trip_id),
			FOREIGN KEY (from_stop_id) REFERENCES stops (stop_id),
			FOREIGN KEY (to_stop_id) REFERENCES stops (stop_id)
		);

		-- Table: pathways
		CREATE TABLE IF NOT EXISTS pathways (
			pathway_id TEXT PRIMARY KEY,
			from_stop_id TEXT NOT NULL DEFAULT '',
			to_stop_id TEXT NOT NULL DEFAULT '',
			pathway_mode INTEGER NOT NULL DEFAULT 0,
			is_bidirectional INTEGER NOT NULL DEFAULT 0,
			length REAL DEFAULT 0.0,
			traversal_time INTEGER DEFAULT 0,
			stair_count INTEGER DEFAULT 0,
			max_slope REAL DEFAULT 0.0,
			min_width REAL DEFAULT 0.0,
			signposted_as TEXT DEFAULT '',
			reversed_signposted_as TEXT DEFAULT '',
			FOREIGN KEY (from_stop_id) REFERENCES stops (stop_id),
			FOREIGN KEY (to_stop_id) REFERENCES stops (stop_id)
		);

		-- Table: levels
		CREATE TABLE IF NOT EXISTS levels (
			level_id TEXT PRIMARY KEY,
			level_index REAL NOT NULL DEFAULT 0.0,
			level_name TEXT DEFAULT ''
		);

		-- Table: feed_info
		CREATE TABLE IF NOT EXISTS feed_info (
			feed_publisher_name TEXT NOT NULL DEFAULT '',
			feed_publisher_url TEXT NOT NULL DEFAULT '',
			feed_lang TEXT NOT NULL DEFAULT '',
			default_lang TEXT DEFAULT '',
			feed_start_date TEXT DEFAULT '',
			feed_end_date TEXT DEFAULT '',
			feed_version TEXT DEFAULT '',
			feed_contact_email TEXT DEFAULT '',
			feed_contact_url TEXT DEFAULT ''
		);

	`

	_, err := v.db.Exec(query)
	if err != nil {
		log.Panicf("%s", err.Error())
	}

}

func (v Database) deleteOldData() error {
	// Query to get all table names from the sqlite_master table
	rows, err := v.db.Query("SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		return fmt.Errorf("failed to fetch tables: %w", err)
	}
	defer rows.Close()

	// Iterate over the tables and delete data from each
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return fmt.Errorf("failed to scan table name: %w", err)
		}

		// Skip system tables that don't need data deletion
		if tableName == "sqlite_sequence" || tableName == "sqlite_master" {
			continue
		}

		// Delete data from the table
		query := fmt.Sprintf("DELETE FROM %s", tableName)
		_, err := v.db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to delete data from table %s: %w", tableName, err)
		}
	}

	fmt.Println("Old data deleted successfully")
	return nil
}

func (v Database) getTableColumns(tableName string) ([]string, error) {
	db := v.db

	// Validate the table name using a regex for valid SQLite table name characters
	validTableName := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	if !validTableName.MatchString(tableName) {
		return nil, fmt.Errorf("invalid table name: %s", tableName)
	}

	// Construct the query string with the sanitized table name
	query := fmt.Sprintf(`PRAGMA table_info(%s);`, tableName)

	// Include all fields returned by PRAGMA table_info, with sql.NullString for nullable fields
	type ColumnInfo struct {
		CID          int            `db:"cid"`        // Column ID
		Name         string         `db:"name"`       // Column name
		Type         string         `db:"type"`       // Data type
		NotNull      int            `db:"notnull"`    // 1 if NOT NULL, 0 otherwise
		DefaultValue sql.NullString `db:"dflt_value"` // Default value (nullable)
		PK           int            `db:"pk"`         // 1 if primary key, 0 otherwise
	}

	var columnsInfo []ColumnInfo
	err := db.Select(&columnsInfo, query)
	if err != nil {
		return nil, fmt.Errorf("error executing query: %w", err)
	}

	// Extract column names from the results
	columns := make([]string, len(columnsInfo))
	for i, col := range columnsInfo {
		columns[i] = col.Name
	}

	return columns, nil
}

func (v Database) createExtraColumn(tableName string, columnName string) error {
	db := v.db

	// Validate the table name using regex to ensure it contains only valid characters
	validName := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	if !validName.MatchString(tableName) {
		return fmt.Errorf("invalid table name: %s", tableName)
	}

	// Validate the column name using the same regex
	if !validName.MatchString(columnName) {
		return fmt.Errorf("invalid column name: %s", columnName)
	}

	// Construct the SQL query with sanitized table and column names
	alterTableSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s TEXT;`, tableName, columnName)
	fmt.Println("Executing SQL:", alterTableSQL)

	// Execute the query using sqlx
	_, err := db.Exec(alterTableSQL)
	if err != nil {
		return fmt.Errorf("failed to add column %s to table %s: %v", columnName, tableName, err)
	}

	return nil
}

func (v Database) createTableIfNotExists(tableName string, headers []string) {
	db := v.db

	// Validate the table name using regex to ensure it contains only valid characters
	validName := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	if !validName.MatchString(tableName) {
		log.Fatalf("Invalid table name: %s", tableName)
	}

	// Validate and sanitize the headers (column names)
	for _, header := range headers {
		if !validName.MatchString(header) {
			log.Fatalf("Invalid column name: %s", header)
		}
	}

	// Construct columns part of the CREATE TABLE statement
	var columns []string
	for _, header := range headers {
		columns = append(columns, fmt.Sprintf("%s TEXT", header))
	}

	// Construct the CREATE TABLE SQL with sanitized table and column names
	createTableSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (%s);`, tableName, strings.Join(columns, ", "))
	fmt.Println("Executing SQL:", createTableSQL)

	// Execute the table creation SQL
	_, err := db.Exec(createTableSQL)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	// Create index for columns ending with "_id"
	for _, header := range headers {
		if strings.HasSuffix(header, "_id") {
			// Sanitize the index name as well
			indexName := fmt.Sprintf("idx_%s_%s", tableName, header)
			if !validName.MatchString(indexName) {
				log.Fatalf("Invalid index name: %s", indexName)
			}
			indexSQL := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (%s);`, indexName, tableName, header)
			fmt.Println("Executing SQL:", indexSQL)

			_, err := db.Exec(indexSQL)
			if err != nil {
				log.Fatalf("Failed to create index on column %s: %v", header, err)
			}
		}
	}
}

func (v Database) refreshDatabaseData() {
	fmt.Println("Updating database data...")

	err := v.deleteOldData()
	if err != nil {
		log.Printf("Failed to delete old data: %v \n(Old data may not exist yet)", err)
	}

	v.createDefaultGTFSTables()
	v.createIndexes()

	// Fetch and write new data
	data, err := fetchZip(v.url)
	if err != nil {
		log.Fatalf("Failed to fetch new data: %v", err)
	}
	err = writeFilesToDB(data, v)
	if err != nil {
		log.Fatalf("Failed to write new data to the database: %v", err)
	}

	fmt.Println("Data updated successfully.")
}

func (v Database) createIndexes() {
	query := `
		-- Indexes for agency table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_agency_agency_id ON agency (agency_id);

		-- Indexes for stops table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_stops_stop_id ON stops (stop_id);
		CREATE INDEX IF NOT EXISTS idx_stops_zone_id ON stops (zone_id);
		CREATE INDEX IF NOT EXISTS idx_stops_parent_station ON stops (parent_station);

		-- Indexes for routes table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_routes_route_id ON routes (route_id);
		CREATE INDEX IF NOT EXISTS idx_routes_agency_id ON routes (agency_id);
		CREATE INDEX IF NOT EXISTS idx_routes_route_color ON routes (route_color);

		-- Indexes for trips table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_trips_trip_id ON trips (trip_id);
		CREATE INDEX IF NOT EXISTS idx_trips_service_id ON trips (service_id);
		CREATE INDEX IF NOT EXISTS idx_trips_route_id ON trips (route_id);

		-- Indexes for stop_times table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_stop_times_trip_id_sequence ON stop_times (trip_id, stop_sequence);
		CREATE INDEX IF NOT EXISTS idx_stop_times_stop_id ON stop_times (stop_id);

		-- Indexes for calendar table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_calendar_service_id ON calendar (service_id);
		CREATE INDEX IF NOT EXISTS idx_calendar_start_end_date ON calendar (start_date, end_date);

		-- Indexes for calendar_dates table
		CREATE INDEX IF NOT EXISTS idx_calendar_dates_date_exception_type ON calendar_dates (date, exception_type);
		CREATE INDEX IF NOT EXISTS idx_calendar_dates_service_id ON calendar_dates (service_id);

		-- Indexes for fare_attributes table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_fare_attributes_fare_id ON fare_attributes (fare_id);
		CREATE INDEX IF NOT EXISTS idx_fare_attributes_agency_id ON fare_attributes (agency_id);

		-- Indexes for fare_rules table
		CREATE INDEX IF NOT EXISTS idx_fare_rules_fare_id ON fare_rules (fare_id);
		CREATE INDEX IF NOT EXISTS idx_fare_rules_route_id ON fare_rules (route_id);

		-- Indexes for shapes table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_shapes_shape_id_sequence ON shapes (shape_id, shape_pt_sequence);

		-- Indexes for frequencies table
		CREATE INDEX IF NOT EXISTS idx_frequencies_trip_id ON frequencies (trip_id);

		-- Indexes for transfers table
		CREATE INDEX IF NOT EXISTS idx_transfers_from_to_stop_id ON transfers (from_stop_id, to_stop_id);

		-- Indexes for pathways table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_pathways_pathway_id ON pathways (pathway_id);
		CREATE INDEX IF NOT EXISTS idx_pathways_from_stop_id ON pathways (from_stop_id);
		CREATE INDEX IF NOT EXISTS idx_pathways_to_stop_id ON pathways (to_stop_id);

		-- Indexes for levels table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_levels_level_id ON levels (level_id);
	`

	_, err := v.db.Exec(query)
	if err != nil {
		log.Panicf("%s", err.Error())
	}
}
