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

---

## ✅ How It Works

### 1. Peer Connection
- A peer (Peer A) starts a gRPC server.
- Another peer (Peer B) connects as a client.
- The peers communicate using RPC calls defined in `filesync.proto`.

---

### 2. Sharing a Folder
- After connection, Peer A can **choose to share a folder**.
- Peer B can accept the share request.

---

### 3. Initial Sync
- Peer A:
  - Scans the shared folder.
  - Sends **each file** (in chunks if needed) using the `SendFile` RPC method.

- Peer B:
  - Receives each file.
  - Writes them to disk, creating a **replica folder** locally.

✅ Now Peer B has an exact copy of Peer A’s shared folder.

---

### 4. Watching for Changes
- Peer B begins watching the replicated folder using `fsnotify`.
- Both Peer A and Peer B are now monitoring their respective folders for:
  - File creation
  - File updates
  - File deletion
  - File renaming

---

### 5. Keeping Files in Sync
- When a file changes in a watched folder:
  - The change is detected by `fsnotify`.
  - The modified file is sent via `SendFile()` to the other peer.
  - The receiving peer overwrites or updates its copy of the file.

This keeps both peers in sync in real-time.

---

## 🧠 Optional: Two-Way Sync & Conflict Resolution

- For full bidirectional syncing:
  - Tag each file change with metadata (origin, timestamp, etc.)
  - Avoid infinite resend loops.
  - Resolve conflicts using a rule (e.g., "last write wins").

---