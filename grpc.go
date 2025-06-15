package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	pb "synthesize/protos"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type server struct {
	pb.UnimplementedFileSyncServiceServer
	user *User
}

func startServer(port string, user *User) {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterFileSyncServiceServer(s, &server{user: user}) // ← Set the user here

	log.Printf("Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
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

		fileChunk := chunk.GetFileChunk() // <- Extract inner FileChunk
		if fileChunk == nil {
			continue
		}

		f, err := os.OpenFile(fileChunk.Filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		_, err = f.Write(fileChunk.Data)
		f.Close()
		if err != nil {
			return err
		}

		fmt.Printf("Received %s from folder %s (chunk #%d)\n", fileChunk.Filename, chunk.Foldername, fileChunk.ChunkNumber)
	}
}

func ShareFolder(folderPath string, client pb.FileSyncServiceClient) error {
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

// create grpc for this
//func (s *server) ReceiveFolder(ctx context.Context, req *pb.FolderMeta) (*pb.Ack, error) {
// Create & store bucket object if not already created
// Add folder to fsnotify watcher
// Create local folder if not already created
// add file to folder
//}

/*
Share bucket function
func (s *server) ShareBucket(dbPath, bucket, peer)
	get each folder in bucket
	write goroutine
		for {
			go send file -> ReceiveFolder to peer
		}
*/

func connectToPeer(ip, name, port string) (pb.FileSyncServiceClient, *grpc.ClientConn) {
	conn, err := grpc.NewClient(ip+":"+port, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Could not connect: %v", err)
	}

	client := pb.NewFileSyncServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
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
	// For simplicity, auto-accept now; you can later build a prompt/UI

	AddPeer(s.user, req.RequesterName, req.RequesterId)
	return &pb.ConnectionResponse{
		Accepted: true,
		Message:  "Connection accepted!",
	}, nil
}

func AddPeer(user *User, peerName, peerIp string) {
	user.Peers[peerIp] = &Peer{
		IPAddress: peerIp,
		Name:      peerName,
	}
	println("Added Peer")
}
