package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	pb "synthesize/protos"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/fsnotify/fsnotify"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type server struct {
	pb.UnimplementedFileSyncServiceServer
	user    *User
	watcher *fsnotify.Watcher
	db      *bolt.DB
}

func NewServer(db *bolt.DB, user *User, watcher *fsnotify.Watcher) *server {
	return &server{
		db:      db,
		user:    user,
		watcher: watcher,
	}
}

func (s *server) SendFile(ctx context.Context, chunk *pb.FileChunk) (*pb.Ack, error) {
	f, err := os.OpenFile(chunk.Filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return &pb.Ack{Received: false, Message: "File open error"}, err
	}
	defer f.Close()

	_, err = f.Write(chunk.Data)
	if err != nil {
		return &pb.Ack{Received: false, Message: "Write error"}, err
	}

	fmt.Printf("Received chunk %d of file %s\n", chunk.ChunkNumber, chunk.Filename)
	if chunk.IsLast {
		fmt.Println("File transfer complete.")
	}

	return &pb.Ack{Received: true, Message: "Chunk received"}, nil
}

func (s *server) ReceiveFolder(stream pb.FileSyncService_ReceiveFolderServer) error {
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.Ack{Received: true, Message: "All chunks received."})
		}
		if err != nil {
			return err
		}

		senderIP := chunk.GetSenderIp()

		s.AddFolderToBucket(chunk.Foldername, "shared_folders", s.watcher)
		err = s.AddUserToSharedFolder(chunk.Foldername, senderIP)
		if err != nil {
			return fmt.Errorf("failed to add sender IP: %w", err)
		}
		if !dirExists(chunk.GetFoldername()) {
			err := os.MkdirAll(chunk.GetFoldername(), 0755)
			if err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			fmt.Println("Directory created:", chunk.GetFoldername())
		}

		fileChunk := chunk.GetFileChunk()
		if fileChunk == nil {
			continue
		}

		// Join the directory with the filename
		fullPath := filepath.Join(chunk.GetFoldername(), fileChunk.Filename)

		// Write to the file
		f, err := os.OpenFile(fullPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer f.Close()

		_, err = f.Write(fileChunk.Data)
		if err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}

		fmt.Printf("Received %s from folder %s (chunk #%d)\n", fileChunk.Filename, chunk.Foldername, fileChunk.ChunkNumber)
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return info.IsDir()
}

func (s *server) ShareFolder(folderPath string, client pb.FileSyncServiceClient) error {
	stream, err := client.ReceiveFolder(context.Background())
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}

	files, err := os.ReadDir(folderPath)
	if err != nil {
		return fmt.Errorf("error reading directory: %w", err)
	}

	for _, entry := range files {
		if entry.IsDir() {
			continue
		}

		filePath := folderPath + "/" + entry.Name()
		f, err := os.Open(filePath)
		if err != nil {
			fmt.Println("Error opening file:", err)
			continue
		}
		defer f.Close()

		buf := make([]byte, 1024)
		chunkNum := int32(1)
		for {
			n, err := f.Read(buf)
			if err != nil && err != io.EOF {
				return err
			}
			isLast := err == io.EOF

			err = stream.Send(&pb.FolderChunk{
				Foldername: folderPath,
				SenderIp:   string(s.user.SelfID),
				FileChunk: &pb.FileChunk{
					Filename:    entry.Name(),
					Data:        buf[:n],
					ChunkNumber: chunkNum,
					IsLast:      isLast,
				},
			})

			if err != nil {
				return fmt.Errorf("failed to send chunk: %w", err)
			}
			chunkNum++
			if isLast {
				break
			}
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("failed to close stream: %w", err)
	}
	fmt.Println("Folder shared:", resp.Message)
	return nil
}

func connectToPeer(ip, name, port string) (pb.FileSyncServiceClient, *grpc.ClientConn) {
	conn, err := grpc.NewClient(ip+":"+port, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Could not connect: %v", err)
	}

	client := pb.NewFileSyncServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	res, err := client.RequestConnection(ctx, &pb.ConnectionRequest{
		RequesterId:   ip,
		RequesterName: name,
	})

	if err != nil {
		log.Printf("Connection request failed: %v", err)
		conn.Close()
		return nil, nil
	}
	if !res.Accepted {
		log.Println("Connection rejected by peer:", res.Message)
		conn.Close()
		return nil, nil
	}

	log.Println("Connection accepted:", res.Message)
	return client, conn
}

func (s *server) RequestConnection(ctx context.Context, req *pb.ConnectionRequest) (*pb.ConnectionResponse, error) {
	fmt.Printf("Incoming connection request from %s (%s)\n", req.RequesterName, req.RequesterId)

	if _, exists := s.user.Peers[req.RequesterId]; exists && s.user.Peers[req.RequesterId].State == "pending" {
		fmt.Printf("Do you want to accept connection (y/n)?\n")

		// CLI loop
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		// add to trusted users
		if input == "y" {
			s.PromotePeerToTrusted(req.RequesterId)

			return &pb.ConnectionResponse{
				Accepted: true,
				Message:  "Connection accepted!",
			}, nil
		}
	}

	// AddPeer(s.user, req.RequesterName, req.RequesterId)
	return &pb.ConnectionResponse{
		Accepted: false,
		Message:  "Connection denied!",
	}, nil
}

func AddPeer(db *bolt.DB, user *User, deviceID, deviceAddress string) error {
	if peer, exists := user.Peers[deviceID]; !exists {
		newPeer := &PeerInfo{
			DeviceID:  PeerID(deviceID),
			Addresses: []string{deviceAddress},
			State:     "seen",
			LastSeen:  time.Now().Unix(),
		}
		user.Peers[deviceID] = newPeer
		log.Printf("Discovered new peer %s at %s", deviceID, deviceAddress)

		if err := UpdatePeer(db, *newPeer); err != nil {
			return fmt.Errorf("failed to persist peer %s: %w", deviceID, err)
		}

	} else if peer.State == "seen" {
		// Update existing "seen" peer with new addr/last seen
		peer.Addresses = appendIfMissing(peer.Addresses, deviceAddress)
		peer.LastSeen = time.Now().Unix()

		if err := UpdatePeer(db, *peer); err != nil {
			print("Error in seen peer")
			return fmt.Errorf("failed to update peer %s: %w", deviceID, err)
		}
	}
	return nil
}

func (s *server) PromotePeerToPending(deviceID string) error {
	peer, ok := s.user.Peers[deviceID]
	if !ok {
		return fmt.Errorf("peer %s not found", deviceID)
	}
	peer.State = "pending"
	return UpdatePeer(s.db, *peer)
}

func (s *server) PromotePeerToTrusted(deviceID string) error {
	peer, ok := s.user.Peers[deviceID]
	if !ok {
		return fmt.Errorf("peer %s not found", deviceID)
	}
	peer.State = "trusted"
	return UpdatePeer(s.db, *peer)
}

func FileUpdateRequest(filePath, IP string, timestamp *timestamppb.Timestamp) (*pb.UpdateResponse, error) {
	// Connect to the peer
	client, conn := connectToPeer(IP, "john", "50051")
	defer conn.Close()

	// Give user 10 seconds to respond
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Send the gRPC request
	resp, err := client.RequestUpdate(ctx, &pb.UpdateRequest{
		FilePath:  filePath,
		IP:        IP,
		Timestamp: timestamp,
	})

	// Handle error or return response
	if err != nil {
		return nil, fmt.Errorf("failed to contact peer %s: %w", IP, err)
	}

	log.Printf("Peer %s responded: accepted=%v, message=%s", IP, resp.GetAccepted(), resp.GetMessage())
	return resp, nil
}

func (s *server) RequestUpdate(ctx context.Context, req *pb.UpdateRequest) (*pb.UpdateResponse, error) {
	filePath := req.FilePath

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}

	localModTime := fileInfo.ModTime()
	remoteTimestamp := req.Timestamp.AsTime()

	if remoteTimestamp.After(localModTime) {
		fmt.Println("Remote version is newer → accept update")
		return &pb.UpdateResponse{Accepted: true, Message: "Will accept update"}, nil
	}

	fmt.Println("Local version is newer or equal → reject update")
	return &pb.UpdateResponse{Accepted: false, Message: "Update not needed"}, nil
}

func (s *server) SendFileUpdate(filePath, IP string) (*pb.UpdateResponse, error) {
	// Connect to the peer
	client, conn := connectToPeer(IP, "john", "50051")
	defer conn.Close()

	// Give user 10 seconds to respond
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	f, err := os.Open(filePath)
	if err != nil {
		fmt.Println("Error opening file:", err)
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil && err != io.EOF {
		return nil, err
	}

	buf := make([]byte, 1024)

	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}

	modTime := fileInfo.ModTime()              // time.Time
	protoTimestamp := timestamppb.New(modTime) // *timestamppb.Timestamp

	// Send the gRPC request
	resp, err := client.ReceiveUpdatedFile(ctx, &pb.FileUpdate{
		Data:      buf[:n],
		FilePath:  filePath,
		Timestamp: protoTimestamp,
	})

	if err != nil {
		return nil, err
	}
	if !resp.Received {
		return &pb.UpdateResponse{Accepted: false, Message: "Update not needed"}, nil
	}

	return &pb.UpdateResponse{Accepted: true, Message: "Update not needed"}, nil
}

func (s *server) ReceiveUpdatedFile(ctx context.Context, req *pb.FileUpdate) (*pb.Ack, error) {
	os.WriteFile(req.FilePath, req.Data, 0644) // Overwrites or creates the file
	return nil, nil
}
