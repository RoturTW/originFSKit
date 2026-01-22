package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
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

type Client struct {
	Token   string
	HTTP    *http.Client
	mu      sync.Mutex
	index   map[string]string
	entries map[string]FileEntry
	dirty   []UpdateChange
	loaded  bool
}

func NewClient(token string) *Client {
	return &Client{
		Token:   token,
		HTTP:    &http.Client{},
		index:   map[string]string{},
		entries: map[string]FileEntry{},
	}
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

	req, _ := http.NewRequest(method, u.String(), r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
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
	if c.loaded {
		return nil
	}
	var raw []any
	if err := c.request("GET", "/files/index", nil, &raw); err != nil {
		return err
	}

	for i := 0; i+entrySize <= len(raw); i += entrySize {
		entry := raw[i : i+entrySize]
		uuid, _ := entry[IdxUUID].(string)
		p := entryToPath(entry)
		c.index[p] = uuid
		c.entries[uuid] = clone(entry)
	}

	c.loaded = true
	return nil
}

func entryToPath(e FileEntry) string {
	return path.Join("/", strings.TrimPrefix(fmt.Sprint(e[IdxLocation]), "/"), fmt.Sprintf("%v%v", e[IdxName], e[IdxType]))
}

func clone(e FileEntry) FileEntry {
	out := make(FileEntry, len(e))
	copy(out, e)
	return out
}

func (c *Client) ListPaths() ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadIndex(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(c.index))
	for p := range c.index {
		out = append(out, p)
	}
	return out, nil
}

func (c *Client) ReadFile(p string) (FileEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadIndex(); err != nil {
		return nil, err
	}
	uuid, ok := c.index[p]
	if !ok {
		return nil, errors.New("not found")
	}
	return clone(c.entries[uuid]), nil
}

func (c *Client) WriteFile(p string, data string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadIndex(); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	if uuid, ok := c.index[p]; ok {
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
	return errors.New("create via CreateFile")
}

func (c *Client) CreateFile(p string, data string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadIndex(); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	dir, file := path.Split(p)
	ext := path.Ext(file)
	name := strings.TrimSuffix(file, ext)
	uuid := fmt.Sprintf("%d", now)
	entry := make(FileEntry, entrySize)
	entry[IdxType] = ext
	entry[IdxName] = name
	entry[IdxLocation] = strings.TrimSuffix(dir, "/")
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

func (c *Client) Remove(p string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadIndex(); err != nil {
		return err
	}
	uuid, ok := c.index[p]
	if !ok {
		return errors.New("not found")
	}
	delete(c.index, p)
	delete(c.entries, uuid)
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDd", UUID: uuid})
	return nil
}

func (c *Client) Rename(oldPath, newPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadIndex(); err != nil {
		return err
	}
	uuid, ok := c.index[oldPath]
	if !ok {
		return errors.New("not found")
	}
	e := c.entries[uuid]
	dir, file := path.Split(newPath)
	ext := path.Ext(file)
	name := strings.TrimSuffix(file, ext)
	now := time.Now().UnixMilli()
	e[IdxType] = ext
	e[IdxName] = name
	e[IdxLocation] = strings.TrimSuffix(dir, "/")
	e[IdxEdited] = now
	c.entries[uuid] = e
	delete(c.index, oldPath)
	c.index[newPath] = uuid
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: ext, Idx: IdxType + 1})
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: name, Idx: IdxName + 1})
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: strings.TrimSuffix(dir, "/"), Idx: IdxLocation + 1})
	c.dirty = append(c.dirty, UpdateChange{Command: "UUIDr", UUID: uuid, Dta: now, Idx: IdxEdited + 1})
	return nil
}

func (c *Client) StatUUID(uuid string) (FileEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loadIndex(); err != nil {
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
	if err := c.request("POST", "/files/update", req, &res); err != nil {
		return err
	}
	c.dirty = nil
	return nil
}
