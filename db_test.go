package main

import (
	"log"
	"os"
	"testing"

	"github.com/fsnotify/fsnotify"
)

/**
func TestCreateBucket(t *testing.T) {
	// Use a test-specific database
	defer os.Remove("test.db") // Clean up after test

	// Run the createBucket function
	err := CreateBucket("files")
	if err != nil {
		t.Fatalf("createBucket failed: %v", err)
	}

	db, err := bolt.Open("test.db", 0600, nil)
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
*/

func TestAddFolderToBucket(t *testing.T) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()
	// Create temp DB
	tmpDB, err := os.CreateTemp("", "test.db")
	if err != nil {
		t.Fatalf("Failed to create temp DB: %v", err)
	}
	defer os.Remove(tmpDB.Name())

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "testdirectory")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Run the function
	err = AddFolderToBucket(tmpDB.Name(), tmpDir, "TestBucket", watcher)
	if err != nil {
		t.Fatalf("AddFolderToBucket failed: %v", err)
	}
}
