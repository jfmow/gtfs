package gtfs

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

type ApiKey struct {
	Header string
	Value  string
}

func fetchZip(url string, apikey ApiKey) ([]byte, error) {
	if url == "" {
		return nil, errors.New("missing url")
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.New("error creating a http request")
	}

	req.Header.Set("Cache-Control", "no-cache")
	if apikey.Header != "" && apikey.Value != "" {
		req.Header.Set(apikey.Header, apikey.Value)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("error making http request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code fetching gtfs zip: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.New("error reading http response body")
	}

	return body, nil
}

var defaultTableNames = []string{
	"agency",
	"stops",
	"routes",
	"trips",
	"stop_times",
	"calendar",
	"calendar_dates",
	"fare_attributes",
	"fare_rules",
	"shapes",
	"frequencies",
	"transfers",
	"pathways",
	"levels",
	"feed_info",
}

func writeFilesToDB(zipData []byte, v Database, tx *sqlx.Tx) error {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return errors.New("error reading GTFS zip file")
	}

	for _, file := range reader.File {
		//fmt.Println("Processing file:", file.Name)

		if file.FileInfo().IsDir() || !isCSVFile(file.Name) {
			fmt.Println("Skipping non-CSV or directory file:", file.Name)
			continue
		}

		var tableName = strings.ToLower(strings.TrimSuffix(filepath.Base(file.Name), ".txt"))

		//fmt.Println("Opening file:", file.Name)
		f, err := file.Open()
		if err != nil {
			return fmt.Errorf("error opening file %s: %v", file.Name, err)
		}
		defer f.Close()

		//fmt.Println("Reading CSV content from file:", file.Name)
		csvReader := csv.NewReader(f)

		// Read file line by line instead of loading all into memory
		headers, err := csvReader.Read()
		if err != nil {
			return fmt.Errorf("error reading csv headers from %s: %v", file.Name, err)
		}
		// Trim spaces from headers, and strip a UTF-8 BOM off the first one
		// (some feeds - e.g. Christchurch's feed_info.txt - ship one, which
		// otherwise becomes part of the column name and breaks the import).
		if len(headers) > 0 {
			headers[0] = strings.TrimPrefix(headers[0], "\ufeff")
		}
		for i := range headers {
			headers[i] = strings.TrimSpace(headers[i])
		}

		//fmt.Println("Headers from file:", headers)

		if !contains(defaultTableNames, tableName) {
			if err := v.createTableIfNotExists(tx, tableName, headers); err != nil {
				return fmt.Errorf("error creating table %s: %v", tableName, err)
			}
		} else {
			columns, err := v.getTableColumns(tx, tableName)
			if err != nil {
				return fmt.Errorf("error getting columns for table %s: %v", tableName, err)
			}
			for _, a := range headers {
				if !contains(columns, a) {
					if err := v.createExtraColumn(tx, tableName, a); err != nil {
						return fmt.Errorf("error adding column %s to table %s: %v", a, tableName, err)
					}
				}
			}
		}

		// Read each record (line by line) and hand it to a batching inserter.
		inserter := newBulkInserter(tx, tableName)
		for {
			record, err := csvReader.Read()
			if err == io.EOF {
				break // End of file
			}
			if err != nil {
				fmt.Println("Error reading record:", err)
				return fmt.Errorf("error reading csv file %s: %v", file.Name, err)
			}

			if err := inserter.add(headers, record); err != nil {
				return fmt.Errorf("error inserting record into %s: %v", tableName, err)
			}
		}
		if err := inserter.flush(); err != nil {
			return fmt.Errorf("error inserting records into %s: %v", tableName, err)
		}

		//fmt.Println("Finished processing file:", file.Name)
	}

	return nil
}

// Keep total placeholders per statement well under any driver/SQLite variable
// limit while still collapsing ~1M single-row inserts into a few thousand.
const maxInsertParams = 20000

// bulkInserter groups CSV rows by which columns they populate (GTFS rows are
// near-homogeneous, so this is a handful of groups) and flushes each group as a
// single multi-row INSERT. Preserves the old "omit empty fields so the column
// DEFAULT applies" semantics exactly - an empty field is never written.
type bulkInserter struct {
	tx    *sqlx.Tx
	table string
	// keyed by comma-joined column list
	groups map[string]*insertGroup
}

type insertGroup struct {
	cols    []string
	rowsPer int
	buf     []interface{}
	pending int
}

func newBulkInserter(tx *sqlx.Tx, table string) *bulkInserter {
	return &bulkInserter{tx: tx, table: table, groups: map[string]*insertGroup{}}
}

func (b *bulkInserter) add(headers []string, record []string) error {
	cols := make([]string, 0, len(record))
	vals := make([]interface{}, 0, len(record))
	for i, value := range record {
		if i >= len(headers) {
			break
		}
		if value != "" {
			cols = append(cols, headers[i])
			vals = append(vals, value)
		}
	}
	if len(cols) == 0 {
		return nil
	}

	key := strings.Join(cols, ",")
	g := b.groups[key]
	if g == nil {
		rowsPer := maxInsertParams / len(cols)
		if rowsPer < 1 {
			rowsPer = 1
		}
		if rowsPer > 1000 {
			rowsPer = 1000
		}
		g = &insertGroup{cols: cols, rowsPer: rowsPer}
		b.groups[key] = g
	}

	g.buf = append(g.buf, vals...)
	g.pending++
	if g.pending >= g.rowsPer {
		return b.flushGroup(g)
	}
	return nil
}

func (b *bulkInserter) flushGroup(g *insertGroup) error {
	if g.pending == 0 {
		return nil
	}

	one := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(g.cols)), ", ") + ")"
	tuples := strings.TrimSuffix(strings.Repeat(one+", ", g.pending), ", ")
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s;",
		b.table, strings.Join(g.cols, ", "), tuples)

	if _, err := b.tx.Exec(sql, g.buf...); err != nil {
		return fmt.Errorf("failed to insert %d rows into table %s: %w", g.pending, b.table, err)
	}
	g.buf = g.buf[:0]
	g.pending = 0
	return nil
}

func (b *bulkInserter) flush() error {
	for _, g := range b.groups {
		if err := b.flushGroup(g); err != nil {
			return err
		}
	}
	return nil
}

func isCSVFile(fileName string) bool {
	return len(fileName) > 4 && fileName[len(fileName)-4:] == ".txt"
}

func GetWorkDir() string {
	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}

	dir := filepath.Dir(ex)

	if strings.Contains(dir, "go-build") {
		return "."
	}
	return filepath.Dir(ex)
}

func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}
