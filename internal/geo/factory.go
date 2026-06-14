package geo

import "fmt"

// New constructs the Locator selected by datastoreType, passing it the opaque,
// driver-specific dsn. This is the one place that knows the set of available
// datastores: adding a new ip2country database is a new implementation plus a
// single case here (e.g. `case "postgres": return newPostgresStore(dsn)`).
func New(datastoreType, dsn string) (Locator, error) {
	switch datastoreType {
	case "csv":
		return newCSVStore(dsn)
	default:
		return nil, fmt.Errorf("unknown datastore type %q", datastoreType)
	}
}
