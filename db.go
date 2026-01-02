package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Struct to hold metadata
type FileMeta struct {
	ModTime time.Time `json:"mod_time"`
	Size    int64     `json:"size"`
}

func InitDB(db *bolt.DB) error {
	return db.Update(func(tx *bolt.Tx) error {
		buckets := []string{"user_identity", "peers", "shared_folders", "user_file_state"}
		for _, name := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("failed creating bucket %s: %w", name, err)
			}
		}
		return nil
	})
}

func loadUsername(db *bolt.DB) (string, error) {
	var name string
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("user_identity"))
		if b == nil {
			return fmt.Errorf("user_identity bucket missing")
		}
		val := b.Get([]byte("name"))
		if val != nil {
			name = string(val)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if name == "" {
		fmt.Print("Enter your username: ")
		fmt.Scanln(&name)
		db.Update(func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("user_identity")).Put([]byte("name"), []byte(name))
		})
	}
	return name, nil
}

func InitPeers(tx *bolt.Tx) error {
	_, err := tx.CreateBucketIfNotExists([]byte("peers"))
	if err != nil {
		return fmt.Errorf("failed to create peers bucket: %w", err)
	}
	return nil
}

func LoadPeers(db *bolt.DB, user *User) error {
	return db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("peers"))
		if b == nil {
			return nil // no peers yet
		}

		return b.ForEach(func(k, v []byte) error {
			var peer PeerInfo
			if err := json.Unmarshal(v, &peer); err != nil {
				return err
			}

			user.Peers[string(k)] = &peer
			return nil
		})
	})
}

func ListPeers(db *bolt.DB) (map[string][]byte, error) {
	peers := make(map[string][]byte)

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("peers"))
		if b == nil {
			return nil // no peers yet
		}

		return b.ForEach(func(k, v []byte) error {
			peers[string(k)] = append([]byte{}, v...)
			return nil
		})
	})

	return peers, err
}

func InitSharedFolders(tx *bolt.Tx) error {
	_, err := tx.CreateBucketIfNotExists([]byte("shared_folders"))
	if err != nil {
		return fmt.Errorf("failed to create shared_folders bucket: %w", err)
	}
	return nil
}

func (s *server) AddPeerToBucket(peer PeerInfo) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("peers"))
		if b == nil {
			return fmt.Errorf("bucket %s does not exist", "peers")
		}

		peerSerialized, err := json.Marshal(peer)
		if err != nil {
			return fmt.Errorf("failed to marshal peer info: %w", err)
		}

		// Use the DeviceID as the key
		return b.Put([]byte(peer.DeviceID), peerSerialized)
	})
}

func UpdatePeer(db *bolt.DB, peer PeerInfo) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("peers"))
		if b == nil {
			return fmt.Errorf("bucket peers does not exist")
		}

		peerSerialized, err := json.Marshal(peer)
		if err != nil {
			return fmt.Errorf("failed to marshal peer info: %w", err)
		}

		return b.Put([]byte(peer.DeviceID), peerSerialized)
	})
}

func (s *server) AddFolderToBucket(folder, bucket string, watcher *fsnotify.Watcher) error {
	err := watcher.Add(folder)
	if err != nil {
		return fmt.Errorf("failed to watch folder %s: %w", folder, err)
	}

	// Store an empty JSON array for IPs
	emptyIPList, err := json.Marshal([]string{})
	if err != nil {
		return fmt.Errorf("failed to marshal empty IP list: %w", err)
	}

	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket %s does not exist", bucket)
		}
		if b.Get([]byte(folder)) == nil {
			b.Put([]byte(folder), emptyIPList)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update DB: %w", err)
	}

	fmt.Printf("Now watching folder: %s\n", folder)

	// GetAllFilesInBucket("my.db", "user_file_state")
	s.AddFilesToFileStateBucket(folder)
	return nil
}

func (s *server) AddFilesToFileStateBucket(folder string) error {
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

		s.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("user_file_state"))
			err := b.Put([]byte(fullPath), data)
			return err
		})
	}

	return nil
}

func (s *server) UpdateFileStateInBucket(path string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("user_file_state"))
		if b == nil {
			fmt.Print("Bucket user_file_state does NOT exist\n")
			return fmt.Errorf("bucket user_file_state does not exist")
		}

		data := b.Get([]byte(path))
		if data == nil {
			fmt.Printf("No data found for key: %q\n", path)
			return nil
		}

		var meta FileMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			return fmt.Errorf("failed to unmarshal file meta: %w", err)
		}

		info, _ := os.Stat(path)
		meta.ModTime = info.ModTime()
		meta.Size = info.Size()

		data, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("failed to marshal IP list: %w", err)
		}

		fmt.Printf("Updated metadata for %q: %+v\n", path, meta)
		return b.Put([]byte(path), data)
	})
}

func (s *server) GetAllFilesInBucket(bucket string) error {
	return s.db.View(func(tx *bolt.Tx) error {
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

func (s *server) AddUserToSharedFolder(folder string, ip string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
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
		print("in add user to shared")

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

func (s *server) GetFoldersInBucket(bucket string) error {
	return s.db.View(func(tx *bolt.Tx) error {
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

func (s *server) NotifySharedFolderUsers(filePath string) error {
	parts := strings.Split(filePath, "/")
	folderName := parts[0]
	fileInfo, _ := os.Stat(filePath)

	modTime := fileInfo.ModTime() // time.Time
	timestamp := timestamppb.New(modTime)

	return s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("shared_folders"))
		if b == nil {
			return fmt.Errorf("bucket %q does not exist", "shared_folders")
		}

		existing := b.Get([]byte(folderName))
		if existing == nil {
			fmt.Printf("No users found for folder: %s\n", folderName)
			return nil
		}

		var userIPs []string
		if err := json.Unmarshal(existing, &userIPs); err != nil {
			return fmt.Errorf("failed to unmarshal existing user list: %w", err)
		}
		var wg sync.WaitGroup

		for _, IP := range userIPs {
			wg.Add(1)
			go func(ip string) {
				defer wg.Done()

				res, err := FileUpdateRequest(filePath, string(s.user.SelfID), ip, timestamp)
				if err != nil {
					fmt.Printf("Error contacting %s: %v\n", ip, err)
					// send future update
					return
				}
				if res.Accepted {
					print("accept file")
					// s.SendFileUpdate(filePath, ip)
				}
			}(IP)
		}

		wg.Wait()
		return nil
	})
}
