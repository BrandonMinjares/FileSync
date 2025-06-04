package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"
)

type FileMeta struct {
	Hash    string
	Size    int64
	ModTime int64 // or time.Time, serialized
	Created int64
}

func updateDB() {
	db, err := bolt.Open("my.db", 0600, nil)
	if err != nil {
		log.Println("Failed to open database:", err)
		os.Exit(1)
	}
	defer db.Close()

	db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket([]byte("MyBucket"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
}

func addFileToDB() {
	db, err := bolt.Open("my.db", 0600, nil)
	if err != nil {
		log.Println("Failed to open database:", err)
		os.Exit(1)
	}
	defer db.Close()

	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("MyBucket"))
		meta := FileMeta{
			Hash:    "abc123...",
			Size:    5120,
			ModTime: time.Now().Unix(),
			Created: time.Now().Unix(),
		}
		encoded, _ := json.Marshal(meta)
		return b.Put([]byte("tmp/test.txt"), encoded)
	})

}
