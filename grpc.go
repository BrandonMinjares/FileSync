package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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

		fullPath := filepath.Join(chunk.GetFoldername(), fileChunk.Filename)

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
				SenderIp:   EncodePeerID(s.user.SelfID),
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

func (s *server) connectToPeer(target string) (pb.FileSyncServiceClient, *grpc.ClientConn) {
	if target == getLocalIP()+":50051" {
		log.Println("Refusing to dial self")
		return nil, nil
	}

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Printf("Could not connect to %s: %v", target, err)
		return nil, nil
	}

	return pb.NewFileSyncServiceClient(conn), conn
}

func (s *server) RequestConnection(ctx context.Context, req *pb.ConnectionRequest) (*pb.ConnectionResponse, error) {
	fmt.Printf("Incoming connection request from %s (%s)\n", req.RequesterName, req.RequesterId)

	if pi, exists := s.user.Peers[req.RequesterId]; exists && pi.State == "seen" {
		if err := s.PromotePeerToPending(req.RequesterId); err != nil {
			log.Printf("failed to promote to trusted: %v", err)
			return &pb.ConnectionResponse{Accepted: false, Message: "Internal error"}, nil
		}
		return &pb.ConnectionResponse{
			Accepted: true,
			Message:  "Connection pending!",
		}, nil
	}

	return &pb.ConnectionResponse{
		Accepted: false,
		Message:  "Connection denied!",
	}, nil
}

func (s *server) FileUpdateRequest(filePath, id, IP string, timestamp *timestamppb.Timestamp) (*pb.UpdateResponse, error) {
	client, conn := s.connectToPeer(IP)
	if conn != nil {
		defer conn.Close()
	}
	if client == nil {
		return nil, fmt.Errorf("connectToPeer failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.RequestUpdate(ctx, &pb.UpdateRequest{
		FilePath:  filePath,
		IP:        IP,
		Timestamp: timestamp,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to contact peer %s: %w", IP, err)
	}

	log.Printf("Peer %s responded: accepted=%v, message=%s", IP, resp.GetAccepted(), resp.GetMessage())
	return resp, nil
}
