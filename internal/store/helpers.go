package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

func jsonUnmarshal(data string, v any) error { return json.Unmarshal([]byte(data), v) }

type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func (s *Store) blobRowsQ(q rowQuerier, id string) ([]byte, []byte, int64, bool, error) {
	row := q.QueryRow(
		`SELECT data, COALESCE(wide_region,x''), COALESCE(wide_start,0), virtual FROM blobs WHERE id=?`, id)
	var data, region []byte
	var wideStart int64
	var virtual int
	if err := row.Scan(&data, &region, &wideStart, &virtual); err != nil {
		return nil, nil, 0, false, err
	}
	return data, region, wideStart, virtual == 1, nil
}

func (s *Store) Blob(ctx context.Context, id string) ([]byte, []byte, int64, bool, error) {
	return s.blobRowsQ(s.db, id)
}
