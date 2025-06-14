package main

import (
	"encoding/binary"
	"fmt"
	pb "synthesize/protos"
	"time"

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

	modTime := time.Now().Unix() // or get it from os.Stat(folder).ModTime().Unix()
	t := make([]byte, 8)
	binary.BigEndian.PutUint64(t, uint64(modTime))

	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		err := b.Put([]byte(folder), t)
		return err
	})

	fmt.Printf("Now watching folder: %s\n", folder)

	return nil
}

func GetFoldersInBucket(dbPath, bucket string) error {
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	return db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			fmt.Printf("Bucket %q does NOT exist\n", bucket)
			return fmt.Errorf("bucket %s does not exist", bucket)
		}

		fmt.Printf("Reading from bucket %q:\n", bucket)
		count := 0
		b.ForEach(func(k, v []byte) error {
			fmt.Printf("  key = %s, value = %s\n", k, v)
			count++
			return nil
		})
		if count == 0 {
			fmt.Printf("Bucket %q is empty\n", bucket)
		}
		return nil
	})
}

func ListAllBuckets(dbPath string) error {
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("failed to open DB: %w", err)
	}
	defer db.Close()

	return db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			fmt.Printf("Bucket: %s\n", name)
			return nil
		})
	})
}

func ShareBucket(dbPath, bucket string, client pb.FileSyncServiceClient) error {
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	return db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %s does not exist", bucket)
		}

		b.ForEach(func(k, v []byte) error {
			folderPath := string(k)
			go ShareFolder(folderPath, client) // share each folder concurrently
			return nil
		})
		return nil
	})
}
