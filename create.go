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

func fetchZip(url string) ([]byte, error) {
	if url == "" {
		return nil, errors.New("missing url")
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.New("error creating a http request")
	}

	req.Header.Set("Cache-Control", "no-cache")
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

func writeFilesToDB(zipData []byte, db *sql.DB) error {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return errors.New("error reading GTFS zip file")
	}

	for _, file := range reader.File {
		fmt.Println("Processing file:", file.Name)

		if file.FileInfo().IsDir() || !isCSVFile(file.Name) {
			fmt.Println("Skipping non-CSV or directory file:", file.Name)
			continue
		}

		if file.Name == "shapes.txt" {
			fmt.Println("Skipping file:", file.Name)
			continue
		}

		fmt.Println("Opening file:", file.Name)
		f, err := file.Open()
		if err != nil {
			return fmt.Errorf("error opening file %s: %v", file.Name, err)
		}
		defer f.Close()

		fmt.Println("Reading CSV content from file:", file.Name)
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

		fmt.Println("Headers from file:", headers)

		// Create table based on headers
		tableName := strings.TrimSuffix(filepath.Base(file.Name), ".txt")
		createTableIfNotExists(db, tableName, headers)

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

		fmt.Println("Finished processing file:", file.Name)
	}

	return nil
}

func createTableIfNotExists(db *sql.DB, tableName string, headers []string) {
	var columns []string
	for _, header := range headers {
		columns = append(columns, fmt.Sprintf("%s TEXT", header))
	}

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
			indexSQL := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_%s ON %s (%s);`, tableName, header, tableName, header)
			fmt.Println("Executing SQL:", indexSQL)

			_, err := db.Exec(indexSQL)
			if err != nil {
				log.Fatalf("Failed to create index on column %s: %v", header, err)
			}
		}
	}
}

func insertRecord(tx *sql.Tx, tableName string, record []CSVRecord) {
	headers := getHeaders(record)
	placeholders := make([]string, len(headers))
	for i := range placeholders {
		placeholders[i] = "?"
	}

	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s);`,
		tableName,
		strings.Join(headers, ", "),
		strings.Join(placeholders, ", "),
	)

	var values []interface{}
	for _, field := range record {
		values = append(values, field.Data)
	}

	//fmt.Println("Inserting record into table:", tableName)
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
