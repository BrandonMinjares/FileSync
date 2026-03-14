package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	pb "synthesize/protos"
)

func (s *server) ReceiveFolder(stream pb.FileSyncService_ReceiveFolderServer) error {
	for {
		chunk, err := stream.Recv()

		if err == io.EOF {
			return stream.SendAndClose(&pb.Ack{
				Received: true,
				Message:  "All chunks received.",
			})
		}

		if err != nil {
			return err
		}

		senderID := chunk.GetSenderIp()

		if err := s.AddFolderToBucket(chunk.Foldername, "shared_folders", s.watcher); err != nil {
			return fmt.Errorf("failed to add folder to bucket: %w", err)
		}

		if err := s.AddUserToSharedFolder(chunk.Foldername, senderID); err != nil {
			return fmt.Errorf("failed to add sender ID: %w", err)
		}

		if !dirExists(chunk.GetFoldername()) {
			if err := os.MkdirAll(chunk.GetFoldername(), 0755); err != nil {
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

		_, err = f.Write(fileChunk.Data)
		closeErr := f.Close()

		if err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}

		if closeErr != nil {
			return fmt.Errorf("failed to close file: %w", closeErr)
		}

		fmt.Printf(
			"Received %s from folder %s (chunk #%d)\n",
			fileChunk.Filename,
			chunk.Foldername,
			fileChunk.ChunkNumber,
		)
	}
}

func (s *server) NotifyTrusted(
	ctx context.Context,
	req *pb.NotifyTrustedRequest,
) (*pb.Ack, error) {

	requesterID := req.GetRequesterId()

	if requesterID == "" {
		return &pb.Ack{
			Received: false,
			Message:  "missing requester_id",
		}, nil
	}

	if err := s.PromotePeerToTrusted(requesterID); err != nil {
		return &pb.Ack{
			Received: false,
			Message:  err.Error(),
		}, nil
	}

	return &pb.Ack{
		Received: true,
		Message:  "Peer marked as trusted",
	}, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)

	if os.IsNotExist(err) {
		return false
	}

	return err == nil && info.IsDir()
}
