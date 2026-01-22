# OriginFS API Endpoints

Base URL: `https://api.rotur.dev`

All requests require `?auth={token}` query parameter.

---

## Endpoints

### `GET /files/path-index`

**Purpose**: Load complete path-to-UUID mapping

**Request**: None

**Response**: `map[string]any`

```json
{
  "index": {
    "origin/(c) users/{username}/path/to/file.txt": "uuid-string",
    "origin/(c) users/{username}/folder": "uuid-string"
  }
}
```

**Response Fields**:

- `index` - Map of full paths to UUID strings

**Used By**: `loadIndex()`

---

### `GET /files/by-uuid?uuid={uuid}`

**Purpose**: Get single file entry by UUID

**Request**: Query parameter `uuid`

**Response**: `FileEntry` (array with 14 elements)

```js
[
  ".txt",              // [0] Type
  "filename",          // [1] Name
  "/path/to",          // [2] Location
  "file content",      // [3] Data
  null,                // [4]
  null,                // [5]
  null,                // [6]
  null,                // [7]
  1704067200000,       // [8] Created (Unix ms)
  1704153600000,       // [9] Edited (Unix ms)
  null,                // [10]
  12345,               // [11] Size
  null,                // [12]
  "uuid-string"        // [13] UUID
]
```

**Response Type**: `FileEntry` (14-element array)

**Used By**: `ensureEntry()`

---

### `POST /files/by-uuid`

**Purpose**: Get multiple file entries by UUIDs (batch fetch)

**Request**: `GetFilesRequest`

```json
{
  "uuids": ["uuid1", "uuid2", "uuid3"]
}
```

**Request Type**:

```go
type GetFilesRequest struct {
    UUIDs []string `json:"uuids"`
}
```

**Response**: `GetFilesResponse`

```js
{
  "files": {
    "uuid1": [/* FileEntry array */],
    "uuid2": [/* FileEntry array */],
    "uuid3": [/* FileEntry array */]
  }
}
```

**Response Type**:

```go
type GetFilesResponse struct {
    Files map[string]FileEntry `json:"files"`
}
```

**Used By**: `ensureEntries()`

---

### `POST /files`

**Purpose**: Commit all pending changes (create/update/delete)

**Request**: `UpdateFileRequest`

```js
{
  "updates": [
    {
      "command": "UUIDa",
      "uuid": "new-uuid",
      "dta": [/* complete FileEntry array */]
    },
    {
      "command": "UUIDr",
      "uuid": "existing-uuid",
      "dta": "new value",
      "idx": 4
    },
    {
      "command": "UUIDd",
      "uuid": "delete-uuid"
    }
  ]
}
```

**Request Type**:

```go
type UpdateFileRequest struct {
    Updates []UpdateChange `json:"updates"`
}

type UpdateChange struct {
    Command string `json:"command"`           // "UUIDa" | "UUIDr" | "UUIDd"
    UUID    string `json:"uuid"`              // Entry UUID
    Dta     any    `json:"dta,omitempty"`     // Data to set
    Idx     any    `json:"idx,omitempty"`     // Array index (1-based!)
}
```

**Commands**:

- `UUIDa` - Add new entry (create)
  - `dta`: Complete FileEntry array
- `UUIDr` - Replace/update field (update)
  - `dta`: New value
  - `idx`: Field index + 1 (1-based indexing!)
- `UUIDd` - Delete entry (delete)
  - No `dta` or `idx` needed

**Response**: `UpdateResult`

```js
{
  "payload": "success message or data"
}
```

**Response Type**:

```go
type UpdateResult struct {
    Payload string `json:"payload"`
}
```

**Used By**: `Commit()`

---

## Request/Response Flow

### Initial Load

1. `GET /files/path-index` → Get all path→UUID mappings
2. Store in `client.index` map

### Read Operations

1. Lookup UUID in `client.index`
2. If entry not cached: `GET /files/by-uuid?uuid={uuid}` → Get FileEntry
3. Store in `client.entries` cache
4. Return cloned entry

### Write Operations

1. Modify `client.entries` locally
2. Append changes to `client.dirty` array
3. Call `Commit()`:
   - `POST /files` with all `client.dirty` changes
   - Clear `client.dirty` on success

---

## Field Index Mapping (1-based for API)

When using `UUIDr` command, add 1 to these indices:

| Go Constant | Array Index | API Index (idx) | Field |
| ------------- | ------------- | ----------------- | ------- |
| `IdxType` | 0 | 1 | Type/Extension |
| `IdxName` | 1 | 2 | Name |
| `IdxLocation` | 2 | 3 | Location |
| `IdxData` | 3 | 4 | Data/Content |
| `IdxCreated` | 8 | 9 | Created |
| `IdxEdited` | 9 | 10 | Edited |
| `IdxSize` | 11 | 12 | Size |
| `IdxUUID` | 13 | 14 | UUID |

**Example**: To update data (IdxData = 3), send `idx: 4`

---

## Example Update Requests

### Create File

```js
{
  "updates": [
    {
      "command": "UUIDa",
      "uuid": "1704067200000",
      "dta": [
        ".txt", "myfile", "/documents", "file content",
        null, null, null, null,
        1704067200000, 1704067200000, null, 12,
        null, "1704067200000"
      ]
    }
  ]
}
```

### Update Content

```js
{
  "updates": [
    {
      "command": "UUIDr",
      "uuid": "existing-uuid",
      "dta": "new content",
      "idx": 4
    },
    {
      "command": "UUIDr",
      "uuid": "existing-uuid",
      "dta": 1704153600000,
      "idx": 10
    },
    {
      "command": "UUIDr",
      "uuid": "existing-uuid",
      "dta": 11,
      "idx": 12
    }
  ]
}
```

### Delete File

```js
{
  "updates": [
    {
      "command": "UUIDd",
      "uuid": "delete-this-uuid"
    }
  ]
}
```

---

## Authentication

All requests include authentication via query parameter:

```txt
?auth={token}
```

Example:

```txt
GET https://api.rotur.dev/files/path-index?auth=your-token-here
```

---

## Error Responses

**Non-200 Status**: Returns error message in body

```json
"error message string"
```

Client converts to: `fmt.Errorf("http %d: %s", statusCode, body)`
