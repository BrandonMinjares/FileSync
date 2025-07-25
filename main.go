package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	pb "synthesize/protos"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Peer struct {
	IPAddress string `json:"ip_address"` // For initiating gRPC connection
	Name      string `json:"name"`       // Optional friendly name
}

/*
*
UserA adds folder to bucket
UserA add connects with UserB
UserA wants to shared folder

	if folder not in UserA's folders:
			create uuid of folder
			add to userA's folders

	send folder, folderid to UserB
	add UserB to SharedWith
*/
type Folder struct {
	FolderID   string
	Path       string
	SharedWith []string
}

type User struct {
	Name    string
	IP      string
	Peers   map[string]*Peer   `json:"peers"`   // Map of peer ID → Peer object
	Folders map[string]*Folder `json:"folders"` // Map of peer ID → Peer object
}

func main() {
	user := &User{
		Name:  "bran",
		IP:    MyPrivateIP,
		Peers: make(map[string]*Peer),
	}

	// bucket that will contain user folders -> metadata
	CreateBucket("my.db", "shared_folders")
	CreateBucket("my.db", "user_file_state")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	reader := bufio.NewReader(os.Stdin)
	go startServer("50051", user, watcher)

	time.Sleep(time.Second * 2) // Wait for the server to spin up

	// Start goroutine to listen for events
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				fmt.Println("EVENT:", event.Name)
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

					parentDir := filepath.Dir(event.Name)

					// notify shared folder
					NotifySharedFolderUsers(parentDir)
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
				2. Add a folder to a bucket
				3. Connect to a new user
				4. List all folders in bucket
				5. List connected users
				6. List all buckets
				7. Share folder with user
				Type "exit" to quit
				> `)

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "2":
			fmt.Print("What folder would you like to add? ")
			folder, _ := reader.ReadString('\n')
			folder = strings.TrimSpace(folder)

			if err := AddFolderToBucket("my.db", folder, "shared_folders", watcher); err != nil {
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
			GetFoldersInBucket("my.db", "shared_folders")

		case "5":
			for key := range user.Peers {
				fmt.Printf("Peer: %s, Name: %s", user.Peers[key].IPAddress, user.Peers[key].Name)
			}

		case "7":
			fmt.Println("Connected peers:")
			i := 1
			peerIPs := []string{}
			for ip, peer := range user.Peers {
				fmt.Printf("%d. %s (%s)\n", i, peer.Name, ip)
				peerIPs = append(peerIPs, ip)
				i++
			}

			fmt.Print("Enter the number of the peer to share the folder with: ")
			choiceStr, _ := reader.ReadString('\n')
			choiceStr = strings.TrimSpace(choiceStr)
			choice, _ := strconv.Atoi(choiceStr)
			if choice < 1 || choice > len(peerIPs) {
				fmt.Println("Invalid choice.")
				continue
			}
			peerIP := peerIPs[choice-1]
			peer := user.Peers[peerIP]

			client, conn := connectToPeer(peer.IPAddress, peer.Name, "50051")
			if client == nil {
				log.Fatal("Failed to connect to peer")
			}
			defer conn.Close()

			fmt.Print("What folder would you like to share? ")
			GetFoldersInBucket("my.db", "shared_folders")
			folder, _ := reader.ReadString('\n')
			folder = strings.TrimSpace(folder)

			// Now share a folder (e.g., "tmp")
			err := ShareFolder(folder, client)
			if err != nil {
				log.Fatalf("Error sharing folder: %v", err)
			}
			AddUserToSharedFolder(folder, peer.IPAddress)

		default:
			fmt.Println("Exiting program")
			return
		}

	}
}
