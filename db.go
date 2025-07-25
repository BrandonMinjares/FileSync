package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	bolt "go.etcd.io/bbolt"
)

// Struct to hold metadata
type FileMeta struct {
	ModTime time.Time `json:"mod_time"`
	Size    int64     `json:"size"`
}

func CreateBucket(db *bolt.DB, bucketName string) error {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return fmt.Errorf("failed to create bucket '%s': %w", "files", err)
		}
		return nil
	})
	return err
}

func AddFolderToBucket(db *bolt.DB, folder, bucket string, watcher *fsnotify.Watcher) error {
	err := watcher.Add(folder)
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
		b.Put([]byte(folder), emptyIPList)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update DB: %w", err)
	}

	fmt.Printf("Now watching folder: %s\n", folder)

	// GetAllFilesInBucket("my.db", "user_file_state")
	AddFilesToFileStateBucket(db, folder)
	return nil
}

func AddFilesToFileStateBucket(db *bolt.DB, folder string) error {
	// Add each file -> metadata to user_file bucket
	entries, err := os.ReadDir(folder)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			fmt.Println("File:", folder+"/"+entry.Name())
		}

		fullPath := filepath.Join(folder, entry.Name())

		info, err := os.Stat(fullPath)
		if err != nil {
			return err
		}

		meta := FileMeta{
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}

		data, err := json.Marshal(meta)
		if err != nil {
			return err
		}

		db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("user_file_state"))
			err := b.Put([]byte(fullPath), data)
			return err
		})
	}

	return nil
}

func GetAllFilesInBucket(dbPath, bucket string) error {
	print("in GetAllFilesInBucket")
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
		fmt.Printf("IP Addresses Sharing Folder: %s\n", string(existing))

		var userIPs []string
		if err := json.Unmarshal(existing, &userIPs); err != nil {
			return fmt.Errorf("failed to unmarshal existing user list: %w", err)
		}

		return nil
	})
}
