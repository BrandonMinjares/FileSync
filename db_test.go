package main

import (
	"os"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestCreateBucket(t *testing.T) {
	// Use a test-specific database
	dbPath := "test.db"
	defer os.Remove(dbPath) // Clean up after test

	// Run the createBucket function
	err := CreateBucket(dbPath, "TestBucket")
	if err != nil {
		t.Fatalf("createBucket failed: %v", err)
	}

	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Check that the bucket exists
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("TestBucket"))
		if b == nil {
			t.Errorf("bucket 'TestBucket' was not created")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to view DB: %v", err)
	}
}
