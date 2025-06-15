package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	pb "synthesize/protos"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Bucket struct {
	Name       string   `json:"name"`        // Unique name of the bucket
	FolderPath string   `json:"folder_path"` // Local absolute path to the folder
	SharedWith []string `json:"shared_with"` // Peer IDs that this bucket is shared with (not including the owner)
}

type Peer struct {
	IPAddress string `json:"ip_address"` // For initiating gRPC connection
	Name      string `json:"name"`       // Optional friendly name
}

type User struct {
	Name    string
	IP      string
	Buckets map[string]*Bucket `json:"buckets"` // Map of bucket name → Bucket object
	Peers   map[string]*Peer   `json:"peers"`   // Map of peer ID → Peer object
}

func main() {
	user := &User{
		Name:    "bran",
		IP:      MyPrivateIP,
		Buckets: make(map[string]*Bucket),
		Peers:   make(map[string]*Peer),
	}

	reader := bufio.NewReader(os.Stdin)
	go startServer("50051", user)

	time.Sleep(time.Second * 2) // Wait for the server to spin up

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// Start goroutine to listen for events
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				fmt.Println("EVENT:", event)
				if event.Op&fsnotify.Create == fsnotify.Create {
					fmt.Println("File created:", event.Name)
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					info, err := os.Stat(event.Name)
					if err != nil {
						fmt.Println("Error:", err)
						return
					}
					fmt.Println("File Name:", info.Name())
					fmt.Println("Size (bytes):", info.Size())
					fmt.Println("Last Modified:", info.ModTime())
				}
				if event.Op&fsnotify.Remove == fsnotify.Remove {
					fmt.Println("File deleted:", event.Name)
				}
				if event.Op&fsnotify.Rename == fsnotify.Rename {
					fmt.Println("File renamed:", event.Name)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Println("ERROR:", err)
			}
		}
	}()

	for {
		fmt.Print(`
				Would you like to:
				1. Create a new bucket
				2. Add a folder to a bucket
				3. Connect to a new user
				4. List all folders in bucket
				5. List connected users
				6. List all buckets
				7. Share bucket with user
				Type "exit" to quit
				> `)

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			fmt.Print("What would you like the bucket name to be? ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if err := CreateBucket("my.db", input); err != nil {
				log.Println("Error creating bucket:", err)
			} else {
				fmt.Println("Bucket created.")
			}

		case "2":
			fmt.Print("What folder would you like to add? ")
			folder, _ := reader.ReadString('\n')
			folder = strings.TrimSpace(folder)

			fmt.Print("To which bucket? ")
			bucket, _ := reader.ReadString('\n')
			bucket = strings.TrimSpace(bucket)

			if err := AddFolderToBucket("my.db", folder, bucket, watcher); err != nil {
				log.Println("Error adding folder to bucket:", err)
			} else {
				fmt.Println("Folder added to bucket.")
			}
			fmt.Printf("Watching folder: %s", folder)
			err = watcher.Add(folder)
			if err != nil {
				log.Fatal("Failed to add watcher:", err)
			}
			fmt.Printf("Successfully watching %s", folder)

		case "3":
			fmt.Print("What is the private IP of the peer you want to connect with? ")
			PrivateIP, _ := reader.ReadString('\n')
			PrivateIP = strings.TrimSpace(PrivateIP)

			_, exists := user.Peers[PrivateIP]
			if exists {
				fmt.Println("Peer is already connected")
				continue
			}

			fmt.Print("What is the name of the user? ")
			name, _ := reader.ReadString('\n')
			name = strings.TrimSpace(name)

			client, conn := connectToPeer(PrivateIP, name, "50051")
			defer conn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()

			resp, err := client.SendFile(ctx, &pb.FileChunk{
				Filename:    "test.txt",
				ChunkNumber: 1,
				Data:        []byte("Hello!"),
				IsLast:      true,
			})

			if err != nil {
				fmt.Println("Connection test failed:", err)
			}

			log.Println("Peer responded:", resp)

			// At this point, you can safely store the peer
			user.Peers[PrivateIP] = &Peer{
				Name:      name,
				IPAddress: PrivateIP,
			}
			fmt.Println("Peer successfully connected and added.")
		case "4":
			fmt.Print("To which bucket? ")
			bucket, _ := reader.ReadString('\n')
			bucket = strings.TrimSpace(bucket)
			GetFoldersInBucket("my.db", bucket)

		case "5":
			for key := range user.Peers {
				fmt.Printf("Peer: %s, Name: %s", user.Peers[key].IPAddress, user.Peers[key].Name)
			}
		case "6":
			ListAllBuckets("my.db")
		case "7":
			client, conn := connectToPeer(PrivateIP, "bran", "50051")
			if client == nil {
				log.Fatal("Failed to connect to peer")
			}
			defer conn.Close()

			// Now share a folder (e.g., "tmp")
			err := ShareFolder("tmp", client)
			if err != nil {
				log.Fatalf("Error sharing folder: %v", err)
			}
		default:
			fmt.Println("Exiting program")
			return
		}

	}
}
