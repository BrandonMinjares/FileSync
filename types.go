package main

import "encoding/base32"

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

const ( // Peer States
	SEEN               = "seen"
	TRUSTED            = "trusted"
	PENDING_APPROVAL   = "pending_approval"
	PENDING_ACCEPTANCE = "pending_acceptance"
	REVOKED            = "revoked"
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
