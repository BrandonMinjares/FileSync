package main

import (
	pb "synthesize/protos"

	bolt "go.etcd.io/bbolt"

	"github.com/fsnotify/fsnotify"
)

type server struct {
	db      *bolt.DB
	user    *User
	watcher *fsnotify.Watcher

	// embed generated gRPC interface
	pb.UnimplementedFileSyncServiceServer
}

func NewServer(db *bolt.DB, user *User, watcher *fsnotify.Watcher) *server {
	return &server{
		db:      db,
		user:    user,
		watcher: watcher,
	}
}
