package main

import (
	"encoding/json"
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

func CreateBucket(dbPath string, bucketName string) error {
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return fmt.Errorf("failed to create bucket '%s': %w", "files", err)
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

	// Store an empty JSON array for IPs
	emptyIPList, err := json.Marshal([]string{})
	if err != nil {
		return fmt.Errorf("failed to marshal empty IP list: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %s does not exist", bucket)
		}
		return b.Put([]byte(folder), emptyIPList)
	})
	if err != nil {
		return fmt.Errorf("failed to update DB: %w", err)
	}

	fmt.Printf("Now watching folder: %s\n", folder)
	return nil
}

func AddUserToSharedFolder(folder string, ip string) error {
	db, err := bolt.Open("my.db", 0600, nil)
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("shared_folders"))
		if err != nil {
			return err
		}

		var userIPs []string
		existing := b.Get([]byte(folder))
		if existing != nil {
			if err := json.Unmarshal(existing, &userIPs); err != nil {
				return fmt.Errorf("failed to unmarshal IP list: %w", err)
			}
		}

		// Avoid duplicates
		for _, existingIP := range userIPs {
			if existingIP == ip {
				return nil // already added
			}
		}

		userIPs = append(userIPs, ip)

		data, err := json.Marshal(userIPs)
		if err != nil {
			return fmt.Errorf("failed to marshal IP list: %w", err)
		}

		return b.Put([]byte(folder), data)
	})
}

func GetFoldersInBucket(dbPath, bucket string) error {
	println("in get folders")
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

func NotifySharedFolderUsers(folder string) error {
	fmt.Println("In notify function for folder:", folder)

	db, err := bolt.Open("my.db", 0600, nil)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	return db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("shared_folders"))
		if b == nil {
			return fmt.Errorf("bucket %q does not exist", "shared_folders")
		}

		// Get the existing value
		existing := b.Get([]byte(folder))
		if existing == nil {
			fmt.Printf("No users found for folder: %s\n", folder)
			return nil
		}

		// Debug print raw value
		fmt.Printf("Raw JSON stored: %s\n", string(existing))

		var userIPs []string
		if err := json.Unmarshal(existing, &userIPs); err != nil {
			return fmt.Errorf("failed to unmarshal existing user list: %w", err)
		}

		// Print each IP
		fmt.Printf("Users sharing folder %s:\n", folder)
		for _, ip := range userIPs {
			fmt.Println(" -", ip)
		}

		return nil
	})
}
