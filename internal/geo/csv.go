package geo

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"slices"
	"strings"
)

// rangeRow is one contiguous IP range mapped to a Location. start and end are
// inclusive.
type rangeRow struct {
	start netip.Addr
	end   netip.Addr
	loc   Location
}

// csvStore answers lookups from a slice of IP ranges held in memory, sorted by
// start address. Because real ip2country databases are range-based, this mirrors
// their shape: lookups are an O(log n) binary search, the whole file is read
// once at startup.
type csvStore struct {
	rows []rangeRow // sorted by start ascending, guaranteed non-overlapping
}

// newCSVStore loads and indexes the CSV file at path. The expected format is one
// range per line: "start_ip,end_ip,country,city". Lines beginning with '#' are
// comments. The file is validated eagerly so a bad datastore fails the service
// at startup rather than on the first request.
func newCSVStore(path string) (*csvStore, error) {
	if path == "" {
		return nil, errors.New("csv datastore: a file path (DATASTORE_DSN) is required")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("csv datastore: %w", err)
	}
	defer f.Close()

	rows, err := parseRanges(f)
	if err != nil {
		return nil, fmt.Errorf("csv datastore %q: %w", path, err)
	}

	// Sort by start so we can binary-search, then reject overlaps. With the
	// rows sorted by start, checking each row against only its predecessor is
	// sufficient to prove the whole set is non-overlapping.
	slices.SortFunc(rows, func(a, b rangeRow) int { return a.start.Compare(b.start) })
	if err := checkNoOverlap(rows); err != nil {
		return nil, fmt.Errorf("csv datastore %q: %w", path, err)
	}

	return &csvStore{rows: rows}, nil
}

// Find locates the range containing ip via binary search. It looks for the last
// range whose start is <= ip, then confirms ip <= that range's end; if ip falls
// in a gap between ranges (or outside all of them) it returns ErrNotFound.
func (s *csvStore) Find(_ context.Context, ip netip.Addr) (Location, error) {
	// pos is the position of the first row with start >= ip. The candidate range
	// (largest start <= ip) is therefore at pos (exact start match) or pos-1.
	pos, exactStart := slices.BinarySearchFunc(s.rows, ip, func(r rangeRow, target netip.Addr) int {
		return r.start.Compare(target)
	})

	idx := pos
	if !exactStart {
		if pos == 0 {
			return Location{}, ErrNotFound // ip is below every range
		}
		idx = pos - 1
	}

	row := s.rows[idx]
	if ip.Compare(row.start) >= 0 && ip.Compare(row.end) <= 0 {
		return row.loc, nil
	}
	return Location{}, ErrNotFound // ip is in a gap above row.start
}

// parseRanges reads and validates every record from r.
func parseRanges(r io.Reader) ([]rangeRow, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = 4 // start, end, country, city — anything else is rejected
	cr.Comment = '#'
	cr.TrimLeadingSpace = true

	var rows []rangeRow
	for n := 1; ; n++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", n, err)
		}
		row, err := parseRow(rec)
		if err != nil {
			return nil, fmt.Errorf("record %d %v: %w", n, rec, err)
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return nil, errors.New("no records found")
	}
	return rows, nil
}

// parseRow validates a single CSV record into a rangeRow.
func parseRow(rec []string) (rangeRow, error) {
	start, err := netip.ParseAddr(strings.TrimSpace(rec[0]))
	if err != nil {
		return rangeRow{}, fmt.Errorf("invalid start ip: %w", err)
	}
	end, err := netip.ParseAddr(strings.TrimSpace(rec[1]))
	if err != nil {
		return rangeRow{}, fmt.Errorf("invalid end ip: %w", err)
	}
	if start.BitLen() != end.BitLen() {
		return rangeRow{}, fmt.Errorf("start %s and end %s are different address families", start, end)
	}
	if start.Compare(end) > 0 {
		return rangeRow{}, fmt.Errorf("start %s is greater than end %s", start, end)
	}

	country := strings.TrimSpace(rec[2])
	if country == "" {
		return rangeRow{}, errors.New("country is empty")
	}
	city := strings.TrimSpace(rec[3]) // city may legitimately be empty

	return rangeRow{start: start, end: end, loc: Location{Country: country, City: city}}, nil
}

// checkNoOverlap requires rows to be sorted by start and rejects any pair of
// overlapping ranges (which would make a lookup ambiguous).
func checkNoOverlap(rows []rangeRow) error {
	for i := 1; i < len(rows); i++ {
		prev, cur := rows[i-1], rows[i]
		if cur.start.Compare(prev.end) <= 0 {
			return fmt.Errorf("overlapping ranges [%s-%s] and [%s-%s]",
				prev.start, prev.end, cur.start, cur.end)
		}
	}
	return nil
}
