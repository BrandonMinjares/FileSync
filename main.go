package main

import (
	"bufio"
	"context"
	"encoding/base32"
	"encoding/json"
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

const ( // Peer States
	SEEN               = "seen"
	TRUSTED            = "trusted"
	PENDING_APPROVAL   = "pending_approval"
	PENDING_ACCEPTANCE = "pending_acceptance"
	REVOKED            = "revoked"
)

type PeerID []byte // ed25519.PublicKey bytes

type Folder struct {
	FolderID   string
	Path       string
	SharedWith []string
}

type User struct {
	Name    string
	SelfID  PeerID
	Peers   map[string]*PeerInfo // key = deviceID (base32 string)
	Folders map[string]*Folder   `json:"folders"` // Map of peer ID → Peer object
}

type PeerInfo struct {
	DeviceID  string   `json:"device_id"` // BASE32 STRING
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	State     string   `json:"state"`
	LastSeen  int64    `json:"last_seen"`
}

const (
	mcastAddr = "239.42.42.42:50000" // multicast group address + port
)

func EncodePeerID(id PeerID) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(id)
}

func DecodePeerID(s string) (PeerID, error) {
	data, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		return nil, err
	}
	return PeerID(data), nil
}

func getLocalIP() string {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "127.0.0.1"
}

// announcePresence broadcasts this peer’s DeviceID and listening port
func announcePresence(deviceID, port string) {
	addr, err := net.ResolveUDPAddr("udp", mcastAddr)
	if err != nil {
		log.Printf("ResolveUDPAddr error: %v", err)
		return
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Printf("DialUDP error: %v", err)
		return
	}
	defer conn.Close()

	msg := map[string]string{
		"device_id": deviceID,
		"addr":      getLocalIP() + ":" + fmt.Sprint(port),
	}

	data, _ := json.Marshal(msg)
	_, _ = conn.Write(data)
}

// listenForPresence listens for other peers’ broadcasts
func listenForPresence(user *User, db *bolt.DB) error {
	addr, err := net.ResolveUDPAddr("udp", mcastAddr)
	if err != nil {
		return err
	}

	conn, err := net.ListenMulticastUDP("udp", nil, addr)
	if err != nil {
		return err
	}
	conn.SetReadBuffer(1024)

	for {
		buf := make([]byte, 4096)
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("ReadFromUDP error: %v", err)
			continue
		}

		var msg map[string]string
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}

		deviceID := msg["device_id"]
		addr := msg["addr"]

		// Ignore self-announcements
		selfID := EncodePeerID(user.SelfID)
		if deviceID == selfID {
			continue
		}

		if err := AddPeer(db, user, deviceID, addr); err != nil {
			log.Printf("failed to persist peer %s: %v", deviceID, err)
		}
	}
}

// helper to avoid duplicates
func appendIfMissing(slice []string, addr string) []string {
	for _, s := range slice {
		if s == addr {
			return slice
		}
	}
	return append(slice, addr)
}

// --- MAIN ---

func main() {
	// Open DB
	dbPath := os.Getenv("DB")
	if dbPath == "" {
		dbPath = "my.db"
	}
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Ensure buckets exist
	if err := InitDB(db); err != nil {
		log.Fatal(err)
	}

	keyPath := os.Getenv("KEYS")
	if keyPath == "" {
		keyPath = "keys"
	}

	kp, err := keys.GenerateOrLoad(keyPath)
	if err != nil {
		log.Fatal(err)
	}

	// Load username from DB
	username, err := loadUsername(db)
	if err != nil {
		log.Fatal(err)
	}

	user := &User{
		Name:   username,
		SelfID: PeerID(kp.Public), // cryptographic device ID
		Peers:  make(map[string]*PeerInfo),
	}

	err = LoadPeers(db, user)
	if err != nil {
		log.Fatal(err)
	}

	// Watches for changes in File State
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	s := grpc.NewServer()
	srv := NewServer(db, user, watcher)

	pb.RegisterFileSyncServiceServer(s, srv)

	port := os.Getenv("PORT")
	if port == "" {
		port = "50051"
	}

	// Start gRPC server
	go func() {
		lis, err := net.Listen("tcp", ":"+port)
		if err != nil {
			log.Fatalf("Failed to listen: %v", err)
		}
		log.Println("Starting gRPC server on port " + port)
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	id := EncodePeerID(user.SelfID) // base32 string
	fmt.Printf("My Device ID: %s\n", id)

	// Start announcer
	go func() {
		for {
			announcePresence(id, port)
			time.Sleep(2 * time.Second)
		}
	}()

	// Start listener
	go func() {
		if err := listenForPresence(user, db); err != nil {
			log.Printf("Presence listener error: %v", err)
		}
	}()

	// Start watching for file system events and handle them
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
					fmt.Printf("Modified or created file: %s\n", event.Name)
					// Update local DB state
					if err := srv.UpdateFileStateInBucket(event.Name); err != nil {
						log.Printf("Failed to update file state: %v", err)
					}
					// Notify peers about the change
					if err := srv.NotifySharedFolderUsers(event.Name); err != nil {
						log.Printf("Failed to notify peers: %v", err)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Watcher error: %v", err)
			}
		}
	}()

	// CLI loop

	reader := bufio.NewReader(os.Stdin)
	// Wait for the server to spin up
	time.Sleep(time.Second * 2)

	for {
		fmt.Print(`
				Would you like to:
				1. Add a folder to a bucket
				2. Connect to a new user
				3. Share folder with user
				4. List connected users
				5. List all folders in bucket
				6. Approve pending connections
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
			fmt.Println("Known peers on receiver:")
			for id, p := range user.Peers {
				fmt.Println(id, p.State, p.Addresses)
			}

			fmt.Print("Enter Device ID to connect with: ")
			deviceID, _ := reader.ReadString('\n')
			deviceID = strings.TrimSpace(deviceID)

			peer, ok := user.Peers[deviceID]
			if !ok {
				fmt.Println("Unknown peer")
				break
			}

			client, conn := srv.connectToPeer(peer.Addresses[0])
			if client == nil {
				fmt.Println("Could not connect")
				break
			}
			defer conn.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := client.RequestConnection(ctx, &pb.ConnectionRequest{
				RequesterId:   EncodePeerID(user.SelfID),
				RequesterName: user.Name,
			})
			if err != nil {
				fmt.Println("Connection request failed:", err)
				break
			}

			if !resp.Accepted {
				fmt.Println("Connection denied:", resp.Message)
				break
			}

			if err := srv.PromotePeerToPendingApproval(deviceID); err != nil {
				log.Println("Failed to promote peer:", err)
			} else {
				fmt.Println("Peer connection is now pending")
			}

		case "3":
			fmt.Println("Connected peers:")
			i := 1
			peerIPs := []string{}
			for ip, peer := range user.Peers {
				fmt.Printf("%d. %s %s\n", i, peer.Name, ip)
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

			client, conn := srv.connectToPeer(peer.Addresses[0])
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
			srv.AddUserToSharedFolder(folder, peer.DeviceID)
		case "4":
			fmt.Println("Connected peers:")
			i := 1
			for ip, peer := range user.Peers {
				fmt.Printf("%d. %s %s %s\n", i, peer.Name, ip, peer.State)
				i++
			}

		case "6":
			peers, err := srv.GetPendingPeers()
			if err != nil {
				log.Fatalf("could not get pending peers: %v", err)
			}
			fmt.Println("Pending peers:")
			for index, peer := range peers {
				fmt.Printf("%d) %s\t\t%s\n", index+1, peer.DeviceID, "pending approval")
			}
			fmt.Println("Enter a peer number to approve, or 'q' to return:")

			var choice int
			fmt.Scanln(&choice)

			if choice < 1 || choice > len(peers) {
				fmt.Println("Invalid choice")
				return
			}

			peer := peers[choice-1]
			if err := srv.PromotePeerToTrusted(peer.DeviceID); err != nil {
				log.Printf("failed to promote peer: %v", err)
				return
			}

			if err := srv.notifyPeerTrusted(peer.DeviceID); err != nil {
				log.Printf("peer promoted but gRPC notify failed: %v", err)
			}

		default:
			fmt.Println("Exiting program")
			return
		}

	}
}
