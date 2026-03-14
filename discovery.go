package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"

	bolt "go.etcd.io/bbolt"
)

const (
	mcastAddr = "239.42.42.42:50000" // multicast group address + port
)

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
