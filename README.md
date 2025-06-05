# Distributed File Synchronizer

A peer-to-peer distributed file synchronization tool built in Go. Designed to detect file changes in real time, propagate updates across nodes using gRPC, and maintain consistent state using lightweight metadata storage.

## 🚀 Features

- 📂 **File Monitoring**: Watches directories for changes using `fsnotify`.
- 🔒 **Integrity Check**: Uses SHA-256 hashing to detect content changes.
- 💾 **Persistent State**: Metadata stored in a local embedded key-value database (`bbolt`).
- 📡 **gRPC Communication**: Synchronizes metadata and propagates changes between peers.
- ⚖️ **Conflict Resolution**: Timestamp-based resolution logic.

## 🧪 Tech Stack

- **Language**: Go
- **File Watching**: [fsnotify](https://github.com/fsnotify/fsnotify)
- **Storage**: [bbolt](https://github.com/etcd-io/bbolt)
- **RPC Framework**: [gRPC](https://grpc.io/)
- **Hashing**: SHA-256