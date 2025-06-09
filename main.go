package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Peer struct {
	ID        string
	IP        string
	SharedIDs []string
}

func main() {
	go startServer("50051")

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

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(`
				Would you like to:
				1. Create a new bucket
				2. Add a folder to a bucket
				3. Connect to a new user
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
			fmt.Println("Enter Private IP of user")
			PrivateIP, _ := reader.ReadString('\n')
			PrivateIP = strings.TrimSpace(PrivateIP)

			fmt.Println("Give this user a name")
			PeerID, _ := reader.ReadString('\n')
			PeerID = strings.TrimSpace(PeerID)
		}
	}
}

/*
	client, conn := connectToPeer(PrivateIP, "50051")
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	resp, err := client.SendFile(ctx, &pb.FileChunk{
		Filename:    "yeah.txt",
		ChunkNumber: 2,
		Data:        []byte("Hello!"),
		IsLast:      true,
	})

	if err != nil {
		fmt.Println(err)
	}
	log.Println(resp)
*/
