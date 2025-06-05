package main

import (
	"fmt"
	"log"

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

func AddFolderToBucket(dbPath, folder, bucket string) error {
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	err = watcher.Add(folder)
	if err != nil {
		log.Fatal("Failed to add watcher:", err)
	}

	fmt.Printf("Successfully watching %s\n", folder)

	return nil
}

/*
func addFileToDB(fileName string) {
	db, err := bolt.Open("my.db", 0600, nil)
	if err != nil {
		log.Println("Failed to open database:", err)
		os.Exit(1)
	}
	defer db.Close()
	filePath := fileName
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		log.Fatal(err)
	}

	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("MyBucket"))
		hash, err := hashFileSHA256(filePath)
		if err != nil {
			log.Fatal(err)
		}
		meta := FileMeta{
			Hash:    hash,
			Size:    fileInfo.Size(),
			ModTime: fileInfo.ModTime().Unix(),
			Created: time.Now().Unix(),
		}
		encoded, _ := json.Marshal(meta)
		return b.Put([]byte(filePath), encoded)
	})

}
*/

/*
// hashFileSHA256 takes a file path and returns the SHA-256 hash of its contents.
func hashFileSHA256(filePath string) (string, error) {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Create a new SHA-256 hasher
	hasher := sha256.New()

	// Copy file content into the hasher
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	// Compute the final hash
	hashBytes := hasher.Sum(nil)

	// Encode to hexadecimal string
	return hex.EncodeToString(hashBytes), nil
}
*/
