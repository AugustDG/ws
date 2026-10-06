package remote

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AugustDG/ws/internal/discover"
)

// The link lets ws on a host reach the ws that connected to it. ws ssh
// serves a Unix socket here, ssh forwards it to a path on the host, and
// the host's picker uses it to list this machine's items and to pick one.
// After a pick the host detaches, ssh exits, and ws ssh opens the choice.

// LinkFile, in the host's state dir, holds the forwarded socket's path.
// The newest connection writes it, so it names the machine most recently
// connected from.
const LinkFile = "link"

// linkGlob matches the sockets ssh forwards on hosts.
const linkGlob = "/tmp/ws-link-*.sock"

func newLinkPath() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "/tmp/ws-link-" + hex.EncodeToString(b) + ".sock"
}

type linkRequest struct {
	Op   string         `json:"op"` // "items" or "open"
	Item *discover.Item `json:"item,omitempty"`
}

type linkResponse struct {
	Items []discover.Item `json:"items,omitempty"`
	Error string          `json:"error,omitempty"`
}

// Link is the end of the link on the machine ws ssh runs on.
type Link struct {
	Path string

	items  func() []discover.Item
	ln     net.Listener
	mu     sync.Mutex
	chosen *discover.Item
}

// Listen serves items at path until Close.
func Listen(path string, items func() []discover.Item) (*Link, error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	l := &Link{Path: path, items: items, ln: ln}
	go l.serve()
	return l, nil
}

func (l *Link) serve() {
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			return
		}
		go l.handle(conn)
	}
}

func (l *Link) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	var req linkRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	var resp linkResponse
	switch {
	case req.Op == "items":
		resp.Items = l.items()
	case req.Op == "open" && req.Item != nil:
		l.mu.Lock()
		l.chosen = req.Item
		l.mu.Unlock()
	default:
		resp.Error = fmt.Sprintf("unknown request %q", req.Op)
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

// Chosen is the item picked on the host, if any. Taking it clears it.
func (l *Link) Chosen() (discover.Item, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.chosen == nil {
		return discover.Item{}, false
	}
	it := *l.chosen
	l.chosen = nil
	return it, true
}

func (l *Link) Close() {
	l.ln.Close()
	os.Remove(l.Path)
}

// LinkPath is the socket recorded in the host's state dir, or "" when ws
// ssh never connected to it.
func LinkPath(stateDir string) string {
	b, err := os.ReadFile(filepath.Join(stateDir, LinkFile))
	if err != nil {
		return ""
	}
	return string(b)
}

func call(path string, req linkRequest) (linkResponse, error) {
	var resp linkResponse
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return resp, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return resp, err
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return resp, err
	}
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

// LinkItems asks the machine at the other end of the link for its items,
// marked as seen via it.
func LinkItems(path string) ([]discover.Item, error) {
	resp, err := call(path, linkRequest{Op: "items"})
	for i := range resp.Items {
		resp.Items[i].Via = "local"
	}
	return resp.Items, err
}

// LinkOpen tells the machine at the other end to open it once this host's
// client detaches.
func LinkOpen(path string, it discover.Item) error {
	it.Via = ""
	_, err := call(path, linkRequest{Op: "open", Item: &it})
	return err
}

// PruneLinks removes forwarded sockets nothing listens on any more, other
// than keep. sshd leaves them behind when a connection ends.
func PruneLinks(keep string) { pruneLinks(linkGlob, keep) }

func pruneLinks(glob, keep string) {
	paths, _ := filepath.Glob(glob)
	for _, p := range paths {
		if p == keep {
			continue
		}
		if conn, err := net.DialTimeout("unix", p, 200*time.Millisecond); err == nil {
			conn.Close()
			continue
		}
		_ = os.Remove(p) // fails on other users' sockets, which is fine
	}
}
