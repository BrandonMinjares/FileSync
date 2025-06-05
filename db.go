package main

import (
	"fmt"

	"github.com/fsnotify/fsnotify"
	bolt "go.etcd.io/bbolt"
)

type FileMeta struct {
	ContentHash string
	Size        int64
	ModTime     int64 // or time.Time, serialized
	Created     int64
}

func CreateBucket(dbPath, bucketName string) error {
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return fmt.Errorf("failed to create bucket '%s': %w", bucketName, err)
		}
		return nil
	})
	return err
}

func AddFolderToBucket(dbPath, folder, bucket string, watcher *fsnotify.Watcher) error {
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	err = watcher.Add(folder)
	if err != nil {
		return fmt.Errorf("failed to watch folder %s: %w", folder, err)
	}

	fmt.Printf("Now watching folder: %s\n", folder)

	return nil
}
