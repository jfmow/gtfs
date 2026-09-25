package gtfs

import (
	"database/sql"
	"fmt"
)

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// tidyHeadsigns rewrites station-code headsigns - Metlink's rail feed uses
// "UPPE-All stops" / "WAIK-Express" / "WELL-Non stop", where the code is a
// stop_id - into the station's name: "Upper Hutt", "Waikanae (Express)".
// Codes that don't match a stop are left as they are. Run after each import
// and on startup (for data imported before this existed).
func tidyHeadsigns(db execer) error {
	name := `CASE WHEN s.stop_name LIKE '% Station' THEN substr(s.stop_name, 1, length(s.stop_name) - 8) ELSE s.stop_name END`
	for _, col := range []struct{ table, column string }{{"stop_times", "stop_headsign"}, {"trips", "trip_headsign"}} {
		h := col.table + "." + col.column
		code := "substr(" + h + ", 1, 4)"
		pattern := "substr(" + h + ", 6)"
		query := fmt.Sprintf(`
			UPDATE %[1]s SET %[2]s = (
				SELECT %[3]s || CASE %[5]s
					WHEN 'All stops' THEN ''
					WHEN 'Non stop' THEN ' (Non-stop)'
					ELSE ' (' || %[5]s || ')'
				END
				FROM stops s WHERE s.stop_id = %[4]s
			)
			WHERE %[6]s GLOB '[A-Z][A-Z][A-Z][A-Z]-*'
			  AND EXISTS (SELECT 1 FROM stops s WHERE s.stop_id = %[4]s)`,
			col.table, col.column, name, code, pattern, h)
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("tidying %s: %w", h, err)
		}
	}
	return nil
}
