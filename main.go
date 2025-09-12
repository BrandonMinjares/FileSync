package main

import (
	"bufio"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
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
	Peers  map[string]*PeerInfo // key = deviceID (base32 string)
}

type PeerInfo struct {
	DeviceID  PeerID   `json:"device_id"`
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	State     string   `json:"state"` // "seen", "pending", "trusted"
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
func announcePresence(deviceID string, port int) {
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
	db, err := bolt.Open("my.db", 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Ensure buckets exist
	if err := InitDB(db); err != nil {
		log.Fatal(err)
	}

	// Load or generate keys
	kp, err := keys.GenerateOrLoad("")
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

	// Watches for changes in File State
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	s := grpc.NewServer()
	srv := NewServer(db, user, watcher)
	pb.RegisterFileSyncServiceServer(s, srv)

	// Start gRPC server
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

	id := EncodePeerID(user.SelfID) // base32 string
	fmt.Printf("My Device ID: %s\n", id)

	// Start announcer
	go func() {
		for {
			announcePresence(id, 50051)
			time.Sleep(2 * time.Second)
		}
	}()

	// Start listener
	go func() {
		if err := listenForPresence(user, db); err != nil {
			log.Printf("Presence listener error: %v", err)
		}
	}()

	// CLI loop
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(`
		Would you like to:
		1. List peers
		2. Connect to a peer (promote to pending & send RequestConnection)
		Type "exit" to quit
		> `)

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			for key := range user.Peers {
				fmt.Printf("Peer ID: %s, Name: %s, Addrs: %v, State: %s\n",
					key,
					user.Peers[key].Name,
					user.Peers[key].Addresses,
					user.Peers[key].State)
			}

		case "2":
			fmt.Print("Enter Device ID to connect with: ")
			deviceID, _ := reader.ReadString('\n')
			deviceID = strings.TrimSpace(deviceID)

			// Promote locally to pending
			if err := srv.PromotePeerToPending(deviceID); err != nil {
				log.Printf("Error moving peer to pending: %v", err)
				continue
			}
			fmt.Println("Peer promoted to pending. Sending connection request...")

			// Look up peer info (need IP address from discovery)
			peer, ok := user.Peers[deviceID]
			if !ok || len(peer.Addresses) == 0 {
				log.Printf("No addresses for peer %s", deviceID)
				continue
			}

			// Use the first known address (host:port)
			ip := peer.Addresses[0]
			client, conn := connectToPeer(ip, user.Name, id)
			if client == nil {
				fmt.Println("Failed to establish connection with peer.")
				continue
			}
			_ = conn.Close() // we don't keep it open here; RequestConnection already sent in connectToPeer
			fmt.Println("Connection request sent. Waiting for peer response...")
		default:
			fmt.Println("Exiting program")
			return
		}
	}
}
