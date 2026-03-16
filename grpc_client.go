package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	pb "synthesize/protos"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *server) connectToPeer(addr string) (pb.FileSyncServiceClient, *grpc.ClientConn) {
	conn, err := grpc.Dial(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil
	}

	client := pb.NewFileSyncServiceClient(conn)
	return client, conn
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

		filePath := filepath.Join(folderPath, entry.Name())

		f, err := os.Open(filePath)
		if err != nil {
			fmt.Println("Error opening file:", err)
			continue
		}

		buf := make([]byte, 1024)
		chunkNum := int32(1)

		for {
			n, err := f.Read(buf)

			if err != nil && err != io.EOF {
				f.Close()
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
				f.Close()
				return fmt.Errorf("failed to send chunk: %w", err)
			}

			chunkNum++

			if isLast {
				break
			}
		}

		f.Close()
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("failed to close stream: %w", err)
	}

	fmt.Println("Folder shared:", resp.Message)

	return nil
}

func (s *server) notifyPeerTrusted(peerDeviceID string) error {

	peer, ok := s.user.Peers[peerDeviceID]
	if !ok {
		return fmt.Errorf("peer %s not found", peerDeviceID)
	}

	if len(peer.Addresses) == 0 {
		return fmt.Errorf("peer %s has no known addresses", peerDeviceID)
	}

	target := peer.Addresses[0]

	client, conn := s.connectToPeer(target)
	if client == nil {
		return fmt.Errorf("failed to connect to peer %s at %s", peerDeviceID, target)
	}

	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.NotifyTrusted(ctx, &pb.NotifyTrustedRequest{
		RequesterId: EncodePeerID(s.user.SelfID),
	})

	return err
}

func (s *server) RequestConnection(ctx context.Context, req *pb.ConnectionRequest) (*pb.ConnectionResponse, error) {
	fmt.Printf("Incoming connection request from %s (%s)\n", req.RequesterName, req.RequesterId)

	if pi, exists := s.user.Peers[req.RequesterId]; exists && pi.State == SEEN {
		if err := s.PromotePeerToPendingAcceptance(req.RequesterId); err != nil {
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

func (s *server) FileUpdateRequest(
	filePath string,
	id string,
	ip string,
	timestamp *timestamppb.Timestamp,
) (*pb.UpdateResponse, error) {

	client, conn := s.connectToPeer(ip)

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
		IP:        ip,
		Timestamp: timestamp,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to contact peer %s: %w", ip, err)
	}

	log.Printf(
		"Peer %s responded: accepted=%v message=%s",
		ip,
		resp.GetAccepted(),
		resp.GetMessage(),
	)

	return resp, nil
}
