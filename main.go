package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"synthesize/keys"
	pb "synthesize/protos"
	"time"

	"github.com/fsnotify/fsnotify"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/grpc"
)

type PeerID []byte // ed25519.PublicKey bytes

type User struct {
	Name   string
	SelfID PeerID
}

func main() {
	// Create cryptographic key pair or load if already exist
	kp, err := keys.GenerateOrLoad("") // stores under .filesync
	if err != nil {
		panic(err)
	}

	db, err := bolt.Open("my.db", 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Initialize databases -- if already existed or do not exist return user name
	err = InitDB(db)
	if err != nil {
		log.Fatal(err)
	}

	user := &User{
		// Device id which is cryptographic public key
		SelfID: PeerID(kp.Public),
	}

	// user.Name = loadUsername(db)
	// user.Peers = loadPeers(db)
	// user.Folders = loadFolders(db, watcher)

	// Watches for changes in File State
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	s := grpc.NewServer()
	srv := NewServer(db, user, watcher)
	pb.RegisterFileSyncServiceServer(s, srv)

	go func() {
		lis, err := net.Listen("tcp", ":50051")
		if err != nil {
			log.Fatalf("Failed to listen: %v", err)
		}

		log.Println("Starting gRPC server on port 50051...")
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	reader := bufio.NewReader(os.Stdin)
	// Wait for the server to spin up
	time.Sleep(time.Second * 2)

	// Listen for events
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

					srv.UpdateFileStateInBucket(event.Name)
					srv.NotifySharedFolderUsers(event.Name)
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
				1. Add a folder to a bucket
				2. Connect to a new user
				3. Share folder with user
				4. List connected users
				5. List all folders in bucket
				Type "exit" to quit
				> `)

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			fmt.Print("What folder would you like to add? ")
			folder, _ := reader.ReadString('\n')
			folder = strings.TrimSpace(folder)

			if err := srv.AddFolderToBucket(folder, "shared_folders", watcher); err != nil {
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

		case "2":
			fmt.Print("What is the Device ID of the peer you want to connect with? ")
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
		case "3":
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
			folder, _ := reader.ReadString('\n')
			folder = strings.TrimSpace(folder)

			// Now share a folder (e.g., "tmp")
			err := srv.ShareFolder(folder, client)
			if err != nil {
				log.Fatalf("Error sharing folder: %v", err)
			}
			srv.AddUserToSharedFolder(folder, peer.IPAddress)
		case "4":
			for key := range user.Peers {
				fmt.Printf("Peer: %s, Name: %s", user.Peers[key].IPAddress, user.Peers[key].Name)
			}
		case "5":
			srv.GetFoldersInBucket("shared_folders")
		default:
			fmt.Println("Exiting program")
			return
		}

	}
}
