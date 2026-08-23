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
