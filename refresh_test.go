package gtfs

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func buildGTFSZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("failed to create zip entry %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write zip entry %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
	return buf.Bytes()
}

func newTestDatabase(t *testing.T, name string) Database {
	t.Helper()
	db, err := newDatabase("http://unset", ApiKey{}, name, time.UTC, "hi@example.com")
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	t.Cleanup(func() {
		db.db.Close()
		dbPath := filepath.Join(GetWorkDir(), "gtfs", "gtfs-"+name+".db")
		os.Remove(dbPath)
		os.Remove(dbPath + "-wal")
		os.Remove(dbPath + "-shm")
	})
	return db
}

// TestRefreshDatabaseDataAtomicRollbackOnFailure verifies that a refresh which
// fails partway through (here: a duplicate primary key causing an insert
// error) rolls back entirely, leaving the previously loaded data intact
// rather than a half-deleted/half-populated table.
func TestRefreshDatabaseDataAtomicRollbackOnFailure(t *testing.T) {
	db := newTestDatabase(t, "refreshatomic")

	goodZip := buildGTFSZip(t, map[string]string{
		"routes.txt": "route_id,route_short_name,route_long_name,route_type\nR1,1,Route One,3\n",
	})
	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(goodZip)
	}))
	defer goodServer.Close()

	db.url = goodServer.URL
	if err := db.refreshDatabaseData(); err != nil {
		t.Fatalf("expected initial refresh to succeed, got: %v", err)
	}

	routes, err := db.GetRoutes()
	if err != nil {
		t.Fatalf("expected routes after initial refresh, got error: %v", err)
	}
	if len(routes) != 1 || routes[0].RouteId != "R1" {
		t.Fatalf("expected exactly route R1 after initial refresh, got: %+v", routes)
	}

	// A feed with a duplicate route_id (primary key) will fail partway
	// through the insert of routes.txt.
	badZip := buildGTFSZip(t, map[string]string{
		"routes.txt": "route_id,route_short_name,route_long_name,route_type\nR2,2,Route Two,3\nR2,2,Route Two Dup,3\n",
	})
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(badZip)
	}))
	defer badServer.Close()

	db.url = badServer.URL
	if err := db.refreshDatabaseData(); err == nil {
		t.Fatalf("expected refresh with duplicate primary key to fail, got nil error")
	}

	routes, err = db.GetRoutes()
	if err != nil {
		t.Fatalf("expected old routes to still be queryable after failed refresh, got error: %v", err)
	}
	if len(routes) != 1 || routes[0].RouteId != "R1" {
		t.Fatalf("expected old route R1 to survive a rolled-back refresh, got: %+v", routes)
	}
}

// TestRefreshBatchedInsertPreservesEmptyFieldSemantics checks that the batching
// bulk inserter still omits empty CSV fields so the column DEFAULT applies -
// i.e. an empty integer column reads back as its default 0, not "".
func TestRefreshBatchedInsertPreservesEmptyFieldSemantics(t *testing.T) {
	db := newTestDatabase(t, "batchinsert")

	// Row 1 populates wheelchair_boarding; rows 2 and 3 leave it empty. Rows
	// also vary which optional columns they set, exercising multiple groups.
	zip := buildGTFSZip(t, map[string]string{
		"stops.txt": "stop_id,stop_name,stop_lat,stop_lon,wheelchair_boarding,platform_code\n" +
			"S1,Alpha,-36.1,174.1,1,A\n" +
			"S2,Beta,-36.2,174.2,,\n" +
			"S3,Gamma,-36.3,174.3,,3\n",
		"routes.txt": "route_id,route_short_name,route_long_name,route_type\nR1,1,Route One,3\n",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(zip) }))
	defer srv.Close()
	db.url = srv.URL

	if err := db.refreshDatabaseData(); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	var cnt int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM stops").Scan(&cnt); err != nil || cnt != 3 {
		t.Fatalf("expected 3 stops, got %d (err %v)", cnt, err)
	}

	// Empty wheelchair_boarding must fall back to the column default 0.
	var wb int
	if err := db.db.QueryRow("SELECT wheelchair_boarding FROM stops WHERE stop_id = 'S2'").Scan(&wb); err != nil {
		t.Fatalf("scan S2 wheelchair_boarding: %v", err)
	}
	if wb != 0 {
		t.Fatalf("expected S2 wheelchair_boarding default 0, got %d", wb)
	}
	var got int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM stops WHERE wheelchair_boarding = 0").Scan(&got); err != nil || got != 2 {
		t.Fatalf("expected 2 stops with wheelchair_boarding = 0, got %d (err %v)", got, err)
	}

	// Populated value survives.
	if err := db.db.QueryRow("SELECT wheelchair_boarding FROM stops WHERE stop_id = 'S1'").Scan(&wb); err != nil || wb != 1 {
		t.Fatalf("expected S1 wheelchair_boarding 1, got %d (err %v)", wb, err)
	}

	// n-gram tables populated and indexed without error.
	if err := db.db.QueryRow("SELECT COUNT(*) FROM stop_ngrams").Scan(&cnt); err != nil || cnt == 0 {
		t.Fatalf("expected stop_ngrams populated, got %d (err %v)", cnt, err)
	}
}

// TestNotifierBroadcastsToAllSubscribers verifies that every subscriber
// (e.g. one per GenerateACache consumer) receives a notification on every
// broadcast, not just one of them.
func TestNotifierBroadcastsToAllSubscribers(t *testing.T) {
	n := &notifier{}

	subA := n.Subscribe()
	subB := n.Subscribe()

	n.broadcast()

	select {
	case <-subA:
	default:
		t.Fatalf("expected subscriber A to be notified")
	}
	select {
	case <-subB:
	default:
		t.Fatalf("expected subscriber B to be notified")
	}
}
