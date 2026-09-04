package gtfs

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

type notifier struct {
	mu   sync.Mutex
	subs []chan struct{}
}

// Subscribe returns a new buffered channel that receives a value every time
// broadcast is called. Every subscriber is notified on every refresh cycle.
func (n *notifier) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	n.subs = append(n.subs, ch)
	n.mu.Unlock()
	return ch
}

func (n *notifier) broadcast() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, ch := range n.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func newDatabase(url string, apiKey ApiKey, databaseName string, tz *time.Location, mailToEmail string) (Database, error) {
	if url == "" {
		return Database{}, errors.New("missing url")
	}
	if len(databaseName) < 3 {
		return Database{}, errors.New("database name to short >3")
	}
	if mailToEmail == "" {
		return Database{}, errors.New("missing mailToEmail")
	}

	os.Mkdir(filepath.Join(GetWorkDir(), "gtfs"), os.ModePerm)

	dbPath := filepath.Join(GetWorkDir(), "gtfs", fmt.Sprintf("gtfs-%s.db", databaseName))
	// Per-connection PRAGMAs via the DSN so every pooled connection gets them.
	// synchronous=NORMAL is safe under WAL and much faster; a bounded page
	// cache + in-memory temp store speed up the bulk rebuild and large joins;
	// foreign_keys stays OFF (matching prior behaviour - the schema declares
	// FKs but the loader relies on them not being enforced during import).
	dsn := "file:" + dbPath + "?" + strings.Join([]string{
		"_pragma=busy_timeout(10000)",
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=foreign_keys(0)",
		// 16 MiB page cache per connection (was 64). Steady-state serving does
		// not need the larger cache; a small pool (below) keeps total RSS bounded
		// on memory-constrained hosts. temp_store is left at the default (FILE)
		// so large ORDER BY sorts spill to disk instead of RAM.
		"_pragma=cache_size(-16384)",
	}, "&")

	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		fmt.Println(err)
		panic("Failed to open the database")
	}

	// SQLite allows a single writer; a small bounded pool keeps read
	// concurrency without letting stray idle connections pin old WAL
	// snapshots and block checkpointing (which is how the -wal files grew to
	// hundreds of MB). Each connection carries its own page cache, so the pool
	// size directly multiplies RSS - keep it to 2 (one for a background cron,
	// one for a user request) and let the second close when idle.
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)

	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		panic("Failed to set WAL mode")
	}
	// Fold any oversized WAL left by a previous run back into the main db file
	// so this startup (and its cache-warming queries) don't read through a
	// multi-hundred-MB WAL.
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		log.Printf("gtfs: startup wal_checkpoint failed for %s: %v", databaseName, err)
	}

	// Initialize the Database struct
	database := Database{db: db, url: url, timeZone: tz, mailToEmail: mailToEmail, apiKey: apiKey, name: databaseName}
	database.refreshNotifier = &notifier{}
	return database, nil
}

func (v Database) createDefaultGTFSTables(tx *sqlx.Tx) error {
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

		-- Table: notifications
		CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,    -- Auto-incrementing primary key
			endpoint TEXT NOT NULL,                   -- Make endpoint NOT NULL if required
			p256dh TEXT NOT NULL DEFAULT '',
			auth TEXT NOT NULL DEFAULT '',
			stop TEXT NOT NULL DEFAULT '',
			recent_notifications TEXT DEFAULT '',
			created INTEGER NOT NULL DEFAULT '',
			CONSTRAINT unique_notification UNIQUE (endpoint, p256dh, auth, stop)  -- Composite unique constraint
		);
		CREATE TABLE IF NOT EXISTS stop_ngrams (
			stop_id TEXT NOT NULL,
			ngram TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS route_ngrams (
			route_id TEXT NOT NULL,
			ngram TEXT NOT NULL
		);
		-- ngram indexes are created in createIndexes(), after the tables are
		-- populated, so the bulk n-gram insert isn't slowed by maintaining them.
	`

	_, err := tx.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create default gtfs tables: %w", err)
	}
	return nil
}

func (v Database) deleteOldData(tx *sqlx.Tx) error {
	// Query to get all table names from the sqlite_master table
	rows, err := tx.Query("SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		return fmt.Errorf("failed to fetch tables: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return fmt.Errorf("failed to scan table name: %w", err)
		}

		// Skip system tables that don't need data deletion
		if tableName == "sqlite_sequence" || tableName == "sqlite_master" {
			continue
		}

		tableNames = append(tableNames, tableName)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate tables: %w", err)
	}
	rows.Close()

	for _, tableName := range tableNames {
		// DROP (not DELETE) - O(1) vs deleting millions of rows plus all the
		// index maintenance and WAL churn that comes with it. DDL is
		// transactional in SQLite, so a failed refresh still rolls back to the
		// previous schema+data. createDefaultGTFSTables recreates the standard
		// tables straight after; dynamic per-file tables are recreated during
		// the import.
		query := fmt.Sprintf("DROP TABLE IF EXISTS %q", tableName)
		if _, err := tx.Exec(query); err != nil {
			return fmt.Errorf("failed to drop table %s: %w", tableName, err)
		}
	}

	log.Println("Old data dropped successfully")
	return nil
}

func (v Database) getTableColumns(tx *sqlx.Tx, tableName string) ([]string, error) {
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
	err := tx.Select(&columnsInfo, query)
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

func (v Database) createExtraColumn(tx *sqlx.Tx, tableName string, columnName string) error {
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

	_, err := tx.Exec(alterTableSQL)
	if err != nil {
		return fmt.Errorf("failed to add column %s to table %s: %v", columnName, tableName, err)
	}

	return nil
}

func (v Database) createTableIfNotExists(tx *sqlx.Tx, tableName string, headers []string) error {
	// Validate the table name using regex to ensure it contains only valid characters
	validName := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	if !validName.MatchString(tableName) {
		return fmt.Errorf("invalid table name: %s", tableName)
	}

	// Validate and sanitize the headers (column names)
	sanitizedHeaders := make([]string, len(headers))
	for i, header := range headers {
		if !validName.MatchString(header) {
			sanitizedHeaders[i] = regexp.MustCompile(`[^a-zA-Z0-9_]`).ReplaceAllString(header, "_")
		} else {
			sanitizedHeaders[i] = header
		}
	}
	headers = sanitizedHeaders

	// Construct columns part of the CREATE TABLE statement
	var columns []string
	for _, header := range headers {
		columns = append(columns, fmt.Sprintf("%s TEXT", header))
	}

	// Construct the CREATE TABLE SQL with sanitized table and column names
	createTableSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (%s);`, tableName, strings.Join(columns, ", "))

	// Execute the table creation SQL
	_, err := tx.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create table: %v", err)
	}

	// Create index for columns ending with "_id"
	for _, header := range headers {
		if strings.HasSuffix(header, "_id") {
			// Sanitize the index name as well
			indexName := fmt.Sprintf("idx_%s_%s", tableName, header)
			if !validName.MatchString(indexName) {
				return fmt.Errorf("invalid index name: %s", indexName)
			}
			indexSQL := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (%s);`, indexName, tableName, header)

			_, err := tx.Exec(indexSQL)
			if err != nil {
				return fmt.Errorf("failed to create index on column %s: %v", header, err)
			}
		}
	}
	return nil
}

// refreshDatabaseData fetches a fresh copy of the GTFS feed and swaps it in
// atomically: every delete, schema change, and insert runs inside a single
// transaction, so concurrent readers either see the complete old dataset or
// the complete new one, never a partially emptied/repopulated one, and any
// failure along the way rolls back leaving the previous good data intact.
func (v Database) refreshDatabaseData() error {
	log.Println("Updating database data: " + v.name)

	log.Println("Fetching new gtfs data.")
	data, err := fetchZip(v.url, v.apiKey)
	if err != nil {
		log.Printf("Failed to fetch new data: %v", err)
		return err
	}
	log.Println("Downloaded new gtfs data.")

	tx, err := v.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin refresh transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	log.Println("Dropping old gtfs tables")
	if err := v.deleteOldData(tx); err != nil {
		return fmt.Errorf("failed to drop old data: %w", err)
	}

	if err := v.createDefaultGTFSTables(tx); err != nil {
		return err
	}

	// Indexes are built AFTER the bulk load (see createIndexes call below) -
	// creating them up front means every one of the ~1M stop_times / shapes
	// inserts pays incremental b-tree maintenance on ~45 indexes.
	log.Println("Writing new data to database")
	if err := writeFilesToDB(data, v, tx); err != nil {
		return fmt.Errorf("failed to write new data to the database: %w", err)
	}

	if err := v.populateStopNgrams(tx); err != nil {
		return err
	}
	if err := v.populateRouteNgrams(tx); err != nil {
		return err
	}

	log.Println("Building indexes")
	if err := v.createIndexes(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit refresh transaction: %w", err)
	}
	committed = true

	// Fold the (now large) WAL back into the main file and refresh planner
	// stats, so the very next query - the cache warm-up - isn't reading through
	// a huge WAL against un-analyzed tables.
	if _, err := v.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		log.Printf("gtfs: post-refresh wal_checkpoint failed for %s: %v", v.name, err)
	}
	if _, err := v.db.Exec("PRAGMA optimize;"); err != nil {
		log.Printf("gtfs: post-refresh optimize failed for %s: %v", v.name, err)
	}
	// The daily rebuild DROPs every table and reloads it, which dumps the old
	// pages onto the freelist - SQLite never returns those to the OS on its own
	// (auto_vacuum is off), so the file only ever grows to its high-water mark.
	// A full VACUUM after each refresh reclaims them. Needs ~1x the db size in
	// free disk transiently; runs at 1am under cronMutex with an idle pool.
	if _, err := v.db.Exec("VACUUM;"); err != nil {
		log.Printf("gtfs: post-refresh VACUUM failed for %s: %v", v.name, err)
	}
	// Hand the pages VACUUM just freed (and the rebuild's transient buffers)
	// back to the OS rather than letting the Go runtime sit on them.
	debug.FreeOSMemory()

	fmt.Println("Data updated successfully.")

	// The fresh feed may change today's schedule - drop the short-lived
	// loadTripStopTimes memo so the next plan rebuilds against the new data.
	tripStopTimesMemos.Delete(v.name)

	v.refreshNotifier.broadcast()
	return nil
}

func (v Database) createIndexes(tx *sqlx.Tx) error {
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
		CREATE INDEX IF NOT EXISTS idx_stop_times_trip_id ON stop_times (trip_id);

		-- Additional indexes for query optimization
		CREATE INDEX IF NOT EXISTS idx_routes_trip ON trips (route_id, trip_id); -- Optimizes joining routes and trips
		CREATE INDEX IF NOT EXISTS idx_stop_times_route_stop ON stop_times (stop_id, trip_id); -- Optimizes joining stop_times and trips

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

		-- Indexes for notifications table
		CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_stop ON notifications (stop);

		-- Indexes for the search n-gram tables
		CREATE INDEX IF NOT EXISTS idx_stop_ngrams_stop_id ON stop_ngrams (stop_id);
		CREATE INDEX IF NOT EXISTS idx_stop_ngrams_ngram ON stop_ngrams (ngram);
		CREATE INDEX IF NOT EXISTS idx_route_ngrams_route_id ON route_ngrams (route_id);
		CREATE INDEX IF NOT EXISTS idx_route_ngrams_ngram ON route_ngrams (ngram);
	`

	_, err := tx.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}
	return nil
}

// createIndexesTx wraps createIndexes in its own short-lived transaction, for
// use outside of a refresh (e.g. at startup when the existing data is still
// up to date and only the indexes need ensuring).
func (v Database) createIndexesTx() error {
	tx, err := v.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	if err := v.createIndexes(tx); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := v.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		log.Printf("gtfs: wal_checkpoint after createIndexesTx failed for %s: %v", v.name, err)
	}
	return nil
}

// ngramWriter batches (id, ngram) pairs into multi-row INSERTs.
type ngramWriter struct {
	tx     *sqlx.Tx
	table  string
	idCol  string
	buf    []interface{}
	rows   int
	maxRow int
}

func newNgramWriter(tx *sqlx.Tx, table, idCol string) *ngramWriter {
	return &ngramWriter{tx: tx, table: table, idCol: idCol, maxRow: 1000}
}

func (w *ngramWriter) add(id, ngram string) error {
	w.buf = append(w.buf, id, ngram)
	w.rows++
	if w.rows >= w.maxRow {
		return w.flush()
	}
	return nil
}

func (w *ngramWriter) flush() error {
	if w.rows == 0 {
		return nil
	}
	tuples := strings.TrimSuffix(strings.Repeat("(?, ?), ", w.rows), ", ")
	sql := fmt.Sprintf("INSERT INTO %s(%s, ngram) VALUES %s;", w.table, w.idCol, tuples)
	if _, err := w.tx.Exec(sql, w.buf...); err != nil {
		return fmt.Errorf("failed to insert %d %s rows: %w", w.rows, w.table, err)
	}
	w.buf = w.buf[:0]
	w.rows = 0
	return nil
}

// addNgrams generates every substring (length >= 2) of every word in text and
// feeds the unique ones for this entity to w.
func addNgrams(w *ngramWriter, id string, seen map[string]struct{}, text string) error {
	for _, word := range strings.Fields(strings.ToLower(text)) {
		for start := 0; start < len(word); start++ {
			for end := start + 2; end <= len(word); end++ {
				substr := word[start:end]
				if _, dup := seen[substr]; dup {
					continue
				}
				seen[substr] = struct{}{}
				if err := w.add(id, substr); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (v Database) populateStopNgrams(tx *sqlx.Tx) error {
	if _, err := tx.Exec("DELETE FROM stop_ngrams"); err != nil {
		return fmt.Errorf("failed to clear stop_ngrams: %w", err)
	}

	type Stop struct {
		StopID   string `db:"stop_id"`
		StopName string `db:"stop_name"`
	}

	var stops []Stop
	if err := tx.Select(&stops, "SELECT stop_id, stop_name FROM stops"); err != nil {
		return fmt.Errorf("failed to select stops: %w", err)
	}

	w := newNgramWriter(tx, "stop_ngrams", "stop_id")
	seen := make(map[string]struct{})
	for _, stop := range stops {
		clear(seen)
		if err := addNgrams(w, stop.StopID, seen, stop.StopName); err != nil {
			return err
		}
	}
	if err := w.flush(); err != nil {
		return err
	}

	log.Println("stop_ngrams table populated successfully")
	return nil
}

func (v Database) populateRouteNgrams(tx *sqlx.Tx) error {
	if _, err := tx.Exec("DELETE FROM route_ngrams"); err != nil {
		return fmt.Errorf("failed to clear route_ngrams: %w", err)
	}

	type Route struct {
		RouteID        string `db:"route_id"`
		RouteLongName  string `db:"route_long_name"`
		RouteShortName string `db:"route_short_name"`
	}

	var routes []Route
	if err := tx.Select(&routes, "SELECT route_id, route_long_name, route_short_name FROM routes"); err != nil {
		return fmt.Errorf("failed to select routes: %w", err)
	}

	w := newNgramWriter(tx, "route_ngrams", "route_id")
	seen := make(map[string]struct{})
	for _, route := range routes {
		clear(seen)
		if err := addNgrams(w, route.RouteID, seen, route.RouteLongName+" "+route.RouteID+" "+route.RouteShortName); err != nil {
			return err
		}
	}
	if err := w.flush(); err != nil {
		return err
	}

	log.Println("route_ngrams table populated successfully")
	return nil
}
