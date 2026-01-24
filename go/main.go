package originFSKit

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const BaseURL = "https://api.rotur.dev"
const entrySize = 14

const (
	IdxType     = 0
	IdxName     = 1
	IdxLocation = 2
	IdxData     = 3
	IdxCreated  = 8
	IdxEdited   = 9
	IdxSize     = 11
	IdxUUID     = 13
)

type FileEntry []any

type UpdateChange struct {
	Command string `json:"command"`
	UUID    string `json:"uuid"`
	Dta     any    `json:"dta,omitempty"`
	Idx     any    `json:"idx,omitempty"`
}

type UpdateFileRequest struct {
	Updates []UpdateChange `json:"updates"`
}

type UpdateResult struct {
	Payload string `json:"payload"`
}

type GetFilesRequest struct {
	UUIDs []string `json:"uuids"`
}

type GetFilesResponse struct {
	Files map[string]FileEntry `json:"files"`
}

type Client struct {
	Token    string
	HTTP     *http.Client
	mu       sync.Mutex
	index    map[string]string
	entries  map[string]FileEntry
	dirty    []UpdateChange
	loaded   bool
	username string
}

func NewClient(token string) *Client {
	return &Client{
		Token: token,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
		},
		index:   map[string]string{},
		entries: map[string]FileEntry{},
	}
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func generateUUID(username string) string {
	data := randomString(16) + strconv.FormatInt(time.Now().UnixMilli(), 10) + username
	md5Hash := md5.Sum([]byte(data))
	return hex.EncodeToString(md5Hash[:])
}

func (c *Client) GetUuid(p string) (string, error) {
	if err := c.loadIndex(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	uuid, ok := c.index[strings.ToLower(p)]
	if !ok {
		return "", errors.New("not found")
	}
	return uuid, nil
}

func (c *Client) GetPath(uuid string) (string, error) {
	if err := c.loadIndex(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureEntry(uuid); err != nil {
		return "", err
	}
	entry, ok := c.entries[uuid]
	if !ok {
		return "", errors.New("not found")
	}
	return entryToPath(entry), nil
}

func (c *Client) request(method, p string, body any, out any) error {
	u, _ := url.Parse(BaseURL + p)
	q := u.Query()
	q.Set("auth", c.Token)
	u.RawQuery = q.Encode()

	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, u.String(), r)
	if err != nil {
		return err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, string(b))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) loadIndex() error {
	c.mu.Lock()
	if c.loaded {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	var raw map[string]any
	if err := c.request("GET", "/files/path-index", nil, &raw); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.loaded {
		return nil
	}

	c.username = raw["username"].(string)

	indexData, ok := raw["index"].(map[string]any)
	if !ok {
		return errors.New("invalid index response")
	}

	for k, v := range indexData {
		vStr, ok := v.(string)
		if !ok {
			continue
		}
		c.index[cleanPath(k)] = vStr
	}

	c.loaded = true
	return nil
}

func (c *Client) ensureEntry(uuid string) error {
	if _, ok := c.entries[uuid]; ok {
		return nil
	}

	var entry FileEntry
	if err := c.request("GET", "/files/by-uuid?uuid="+uuid, nil, &entry); err != nil {
		return err
	}

	c.entries[uuid] = entry
	return nil
}

func entryToPath(e FileEntry) string {
	location := fmt.Sprint(e[IdxLocation])
	name := fmt.Sprint(e[IdxName])
	typ := fmt.Sprint(e[IdxType])
	return cleanPath(strings.TrimPrefix(location, "/") + "/" + fmt.Sprintf("%v%v", name, typ))
}

func cleanPath(p string) string {
	p = strings.ToLower(p)
	p = strings.TrimPrefix(p, "origin/(c) users/")

	parts := strings.SplitN(p, "/", 2)
	if len(parts) == 2 {
		p = parts[1]
	} else {
		p = ""
	}

	return path.Clean("/" + p)
}

func clone(e FileEntry) FileEntry {
	out := make(FileEntry, len(e))
	copy(out, e)
	return out
}

func (c *Client) ListPaths() ([]string, error) {
	if err := c.loadIndex(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.index))
	for p := range c.index {
		out = append(out, p)
	}
	return out, nil
}

func (c *Client) ReadFile(p string) (FileEntry, error) {
	if err := c.loadIndex(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	uuid, ok := c.index[strings.ToLower(p)]
	if !ok {
		return nil, errors.New("not found")
	}
	if err := c.ensureEntry(uuid); err != nil {
		return nil, err
	}
	return clone(c.entries[uuid]), nil
}

func (c *Client) ReadFileContent(p string) (string, error) {
	if err := c.loadIndex(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	uuid, ok := c.index[strings.ToLower(p)]
	if !ok {
		return "", errors.New("not found")
	}
	if err := c.ensureEntry(uuid); err != nil {
		return "", err
	}
	data, ok := c.entries[uuid][IdxData].(string)
	if !ok {
		return "", errors.New("invalid data type")
	}
	return data, nil
}

func (c *Client) WriteFile(p string, data string) error {
	if err := c.loadIndex(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UnixMilli()
	uuid, ok := c.index[strings.ToLower(p)]
	if !ok {
		return errors.New("create via CreateFile")
	}
	if err := c.ensureEntry(uuid); err != nil {
		return err
	}
	e := c.entries[uuid]
	e[IdxData] = data
	e[IdxEdited] = now
	e[IdxSize] = len(data)
	c.entries[uuid] = e
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: data, Idx: IdxData + 1})
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: now, Idx: IdxEdited + 1})
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: len(data), Idx: IdxSize + 1})
	return nil
}

func (c *Client) createFolders(dir string) error {
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" || dir == "/" {
		return nil
	}

	parts := strings.Split(dir, "/")
	for i := 1; i <= len(parts); i++ {
		subPath := strings.ToLower(path.Join(parts[:i]...))
		if !strings.HasPrefix(subPath, "/") {
			subPath = "/" + subPath
		}
		if _, ok := c.index[subPath]; !ok {
			now := time.Now().UnixMilli()
			uuid := generateUUID(c.username)
			entry := make(FileEntry, entrySize)
			entry[IdxType] = ".folder"
			entry[IdxName] = parts[i-1]
			entry[IdxLocation] = "origin/(c) users/" + c.username + "/" + strings.TrimPrefix(strings.TrimSuffix(strings.Join(parts[:i-1], "/"), "/"), "/")
			entry[IdxData] = []any{}
			entry[IdxCreated] = now
			entry[IdxEdited] = now
			entry[IdxSize] = 0
			entry[IdxUUID] = uuid
			c.entries[uuid] = entry
			c.index[subPath] = uuid
			c.dirty = append(c.dirty, UpdateChange{Command: "UUIDa", UUID: uuid, Dta: entry})
		}
	}
	return nil
}

func (c *Client) CreateFile(p string, data string) error {
	p = strings.ToLower(p)
	if err := c.loadIndex(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UnixMilli()
	dir, file := path.Split(p)
	ext := path.Ext(file)
	name := strings.TrimSuffix(file, ext)

	if err := c.createFolders(dir); err != nil {
		return err
	}

	uuid := generateUUID(c.username)
	entry := make(FileEntry, entrySize)
	entry[IdxType] = ext
	entry[IdxName] = name
	entry[IdxLocation] = "origin/(c) users/" + c.username + "/" + strings.TrimPrefix(strings.TrimSuffix(dir, "/"), "/")
	entry[IdxData] = data
	entry[IdxCreated] = now
	entry[IdxEdited] = now
	entry[IdxSize] = len(data)
	entry[IdxUUID] = uuid
	c.entries[uuid] = entry
	c.index[p] = uuid
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDa", UUID: uuid, Dta: entry})
	return nil
}

func (c *Client) CreateFolder(p string) error {
	p = strings.ToLower(p)
	if err := c.loadIndex(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UnixMilli()
	dir, file := path.Split(p)
	ext := path.Ext(file)
	name := strings.TrimSuffix(file, ext)

	if err := c.createFolders(dir); err != nil {
		return err
	}

	uuid := generateUUID(c.username)
	entry := make(FileEntry, entrySize)
	entry[IdxType] = ".folder"
	entry[IdxName] = name
	entry[IdxLocation] = strings.TrimSuffix(dir, "/")
	entry[IdxData] = []any{}
	entry[IdxCreated] = now
	entry[IdxEdited] = now
	entry[IdxSize] = 0
	entry[IdxUUID] = uuid
	c.entries[uuid] = entry
	c.index[p] = uuid
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDa", UUID: uuid, Dta: entry})
	return nil
}

func (c *Client) ListDir(p string) []string {
	p = strings.TrimSuffix(strings.ToLower(p), "/")
	if p == "" {
		p = "/"
	}

	paths, err := c.ListPaths()
	if err != nil {
		return []string{}
	}

	children := make(map[string]struct{})

	prefix := p
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	for _, fullPath := range paths {
		fullPath = strings.ToLower(fullPath)
		if strings.HasPrefix(fullPath, prefix) {
			rest := strings.TrimPrefix(fullPath, prefix)
			parts := strings.SplitN(rest, "/", 2)
			child := parts[0]
			children[child] = struct{}{}
		}
	}

	out := make([]string, 0, len(children))
	for k := range children {
		out = append(out, k)
	}

	return out
}

func (c *Client) Remove(p string) error {
	p = strings.ToLower(p)
	if err := c.loadIndex(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	uuid, ok := c.index[p]
	if !ok {
		return errors.New("not found")
	}
	delete(c.index, p)
	delete(c.entries, uuid)
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDd", UUID: uuid})
	return nil
}

func (c *Client) Exists(p string) bool {
	p = strings.ToLower(p)
	if err := c.loadIndex(); err != nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.index[p]
	return ok
}

func (c *Client) JoinPath(elem ...string) string {
	joined := path.Join(elem...)
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	return strings.ToLower(joined)
}

func (c *Client) Rename(oldPath, newPath string) error {
	if err := c.loadIndex(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	uuid, ok := c.index[strings.ToLower(oldPath)]
	if !ok {
		return errors.New("not found")
	}
	if err := c.ensureEntry(uuid); err != nil {
		return err
	}
	e := c.entries[uuid]
	dir, file := path.Split(newPath)
	ext := path.Ext(file)
	name := strings.TrimSuffix(file, ext)
	now := time.Now().UnixMilli()
	e[IdxType] = ext
	e[IdxName] = name
	e[IdxLocation] = "origin/(c) users/" + c.username + "/" + strings.TrimPrefix(strings.TrimSuffix(dir, "/"), "/")
	e[IdxEdited] = now
	c.entries[uuid] = e
	delete(c.index, strings.ToLower(oldPath))
	c.index[strings.ToLower(newPath)] = uuid
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: ext, Idx: IdxType + 1})
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: name, Idx: IdxName + 1})
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: e[IdxLocation], Idx: IdxLocation + 1})
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: now, Idx: IdxEdited + 1})
	return nil
}

func (c *Client) StatUUID(uuid string) (FileEntry, error) {
	if err := c.loadIndex(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureEntry(uuid); err != nil {
		return nil, err
	}
	e, ok := c.entries[uuid]
	if !ok {
		return nil, errors.New("not found")
	}
	return clone(e), nil
}

func (c *Client) Commit() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.dirty) == 0 {
		return nil
	}
	req := UpdateFileRequest{Updates: c.dirty}
	var res UpdateResult
	if err := c.request("POST", "/files", req, &res); err != nil {
		return err
	}
	c.dirty = nil
	return nil
}
