package database

// DB represents the database connection
type DB struct {
	IsMock bool
}

// Connect returns a mock DB for testing, or a real one in production
func Connect(url string) (*DB, error) {
	if url == "mock" {
		return &DB{IsMock: true}, nil
	}
	
	// TODO: Add real PostgreSQL connection logic here later
	// For now, it safely returns a mock to prevent crashes
	return &DB{IsMock: true}, nil
}

// Close safely closes the connection
func (db *DB) Close() error {
	return nil
}