package main

import (
	"context"
	"fmt"
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
}

func startServer(port string) {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterFileSyncServiceServer(s, &server{})
	log.Printf("Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func (s *server) SendFile(ctx context.Context, chunk *pb.FileChunk) (*pb.Ack, error) {
	fmt.Println("hello")
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

func connectToPeer(ip, port string) (pb.FileSyncServiceClient, *grpc.ClientConn) {
	conn, err := grpc.NewClient(ip+":"+port, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Could not connect: %v", err)
	}

	client := pb.NewFileSyncServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	res, err := client.RequestConnection(ctx, &pb.ConnectionRequest{
		RequesterId:   ip,
		RequesterName: "john",
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
	return &pb.ConnectionResponse{
		Accepted: true,
		Message:  "Connection accepted!",
	}, nil
}
