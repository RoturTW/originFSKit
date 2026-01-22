# OriginFSKit API Reference

Go client for OriginFS file system at `https://api.rotur.dev`

## Quick Start

```go
client := originFSKit.NewClient("your-token")
client.CreateFile("/file.txt", "content")
client.Commit() // Required to push changes to the account
```

---

## Types

### FileEntry `[]any`

Array with 14 elements containing file/folder metadata.

| Index | Constant | Type | Description |
| ------- | ---------- | ------ | ------------- |
| 0 | `IdxType` | string | Extension or ".folder" |
| 1 | `IdxName` | string | Name without extension |
| 2 | `IdxLocation` | string | Parent directory path |
| 3 | `IdxData` | string/[]any | Content or children |
| 8 | `IdxCreated` | int64 | Unix timestamp (ms) |
| 9 | `IdxEdited` | int64 | Unix timestamp (ms) |
| 11 | `IdxSize` | int | Bytes |
| 13 | `IdxUUID` | string | Unique identifier |

### Client `struct`

Thread-safe client with local caching. All write operations are batched until `Commit()`.

---

## Initialization

### `NewClient(token string) *Client`

Creates client with 30s timeout.

---

## File Operations

### `ReadFile(p string) (FileEntry, error)`

Returns complete entry with metadata and content.

### `ReadFileContent(p string) (string, error)`

Returns only file content as string.

### `WriteFile(p string, data string) error`

Updates existing file. Auto-updates edited time and size. **Requires Commit()**.

### `CreateFile(p string, data string) error`

Creates new file. Auto-creates parent directories. **Requires Commit()**.

### `Remove(p string) error`

Deletes file or folder. **Requires Commit()**.

### `Rename(oldPath, newPath string) error`

Moves/renames file or folder. **Requires Commit()**.

### `Exists(p string) bool`

Checks if path exists.

---

## Directory Operations

### `CreateFolder(p string) error`

Creates folder. Auto-creates parents. **Requires Commit()**.

### `ListDir(p string) []string`

Returns immediate children names (not full paths).

### `ListPaths() ([]string, error)`

Returns all paths in file system.

---

## Path Utilities

### `JoinPath(elem ...string) string`

Joins and normalizes path (lowercase, leading slash).

### `GetUuid(p string) (string, error)`

Returns UUID for path.

### `GetPath(uuid string) (string, error)`

Returns path for UUID.

### `StatUUID(uuid string) (FileEntry, error)`

Returns entry metadata by UUID.

---

## Synchronization

### `Commit() error`

**Persists all pending changes to server.** Must be called after writes, creates, deletes, or renames.

---

## Common Patterns

```go
// Read
content, _ := client.ReadFileContent("/file.txt")

// Write (requires commit)
client.WriteFile("/file.txt", "new content")
client.Commit()

// Create (requires commit)
client.CreateFile("/new.txt", "data")
client.Commit()

// Batch operations
client.CreateFolder("/backup")
client.CreateFile("/backup/1.txt", "data1")
client.CreateFile("/backup/2.txt", "data2")
client.Commit() // Single commit for all

// Check existence
if client.Exists("/file.txt") {
    client.Remove("/file.txt")
    client.Commit()
}

// Directory listing
children := client.ListDir("/documents")
for _, child := range children {
    path := client.JoinPath("/documents", child)
    // ...
}

// Access metadata
entry, _ := client.ReadFile("/file.txt")
name := entry[IdxName].(string)
size := entry[IdxSize].(int)
created := entry[IdxCreated].(int64)
```

---

## Key Behaviors

- **Paths**: Case-insensitive, normalized to lowercase, auto-prefixed with `/`
- **Caching**: Index loaded lazily on first use, entries cached locally
- **Batching**: All writes batched until `Commit()` called
- **Thread-safety**: Mutex-protected, safe for concurrent use
- **Timeouts**: 30-second HTTP timeout on all requests
- **Auto-creation**: Parent directories created automatically for files/folders
