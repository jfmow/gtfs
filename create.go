package gtfs

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.New("error reading http response body")
	}

	return body, nil
}

type CSVRecord struct {
	Header string
	Data   string
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

func writeFilesToDB(zipData []byte, v Database) error {
	db := v.db
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

		tx, err := db.Begin() // Start transaction for better performance
		if err != nil {
			return fmt.Errorf("error starting transaction: %v", err)
		}

		// Read file line by line instead of loading all into memory
		headers, err := csvReader.Read()
		if err != nil {
			return fmt.Errorf("error reading csv headers from %s: %v", file.Name, err)
		}
		// Trim spaces from headers
		for i := range headers {
			headers[i] = strings.TrimSpace(headers[i])
		}

		//fmt.Println("Headers from file:", headers)

		if !contains(defaultTableNames, tableName) {
			v.createTableIfNotExists(tableName, headers)
		} else {
			columns, err := v.getTableColumns(tableName)
			if err != nil {
				log.Panicln(err)
			}
			for _, a := range headers {
				if !contains(columns, a) {
					v.createExtraColumn(tableName, a)
				}
			}
		}

		// Read each record (line by line)
		for {
			record, err := csvReader.Read()
			if err == io.EOF {
				break // End of file
			}
			if err != nil {
				fmt.Println("Error reading record:", err)
				return fmt.Errorf("error reading csv file %s: %v", file.Name, err)
			}

			// Convert record into CSVRecord for insertion
			var row []CSVRecord
			for i, value := range record {
				row = append(row, CSVRecord{Header: headers[i], Data: value})
			}

			// Insert into DB
			insertRecord(tx, tableName, row)
		}

		// Commit the transaction after processing the file
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("error committing transaction: %v", err)
		}

		//fmt.Println("Finished processing file:", file.Name)
	}

	return nil
}
func insertRecord(tx *sql.Tx, tableName string, record []CSVRecord) {
	headers := getHeaders(record)
	var placeholders []string
	var values []interface{}
	var filteredHeaders []string

	for i, field := range record {
		if field.Data != "" {
			placeholders = append(placeholders, "?")
			values = append(values, field.Data)
			filteredHeaders = append(filteredHeaders, headers[i])
		}
	}

	if len(values) == 0 {
		log.Println("Skipping insert: No valid data in record")
		return
	}

	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s);`,
		tableName,
		strings.Join(filteredHeaders, ", "),
		strings.Join(placeholders, ", "),
	)

	_, err := tx.Exec(insertSQL, values...)
	if err != nil {
		log.Fatalf("Failed to insert record into table %s: %v", tableName, err)
	}
}

func getHeaders(record []CSVRecord) []string {
	var headers []string
	for _, field := range record {
		headers = append(headers, field.Header)
	}
	return headers
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
