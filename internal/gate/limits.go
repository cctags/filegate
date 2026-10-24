package gate

import (
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	maxConnections      = 128
	maxRequests         = 128
	maxDownloads        = 32
	maxDownloadsPerIP   = 4
	maxDirectoryEntries = 10000
	maxCodesPerAlias    = 10
	writeIdleTimeout    = 30 * time.Second
)

type downloadLimiter struct {
	mu    sync.Mutex
	total int
	perIP map[string]int
}

func (l *downloadLimiter) acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= maxDownloads || l.perIP[ip] >= maxDownloadsPerIP {
		return false
	}
	if l.perIP == nil {
		l.perIP = map[string]int{}
	}
	l.total++
	l.perIP[ip]++
	return true
}
func (l *downloadLimiter) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total--
	l.perIP[ip]--
	if l.perIP[ip] == 0 {
		delete(l.perIP, ip)
	}
}

// Limit connections before HTTP parsing, including TLS handshakes. The handler
// separately limits requests, since HTTP/2 can multiplex many on one connection.
func ListenIPv4(address string) (net.Listener, error) {
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, err
	}
	return newLimitedListener(listener, maxConnections), nil
}

type limitedListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}

func newLimitedListener(listener net.Listener, limit int) *limitedListener {
	return &limitedListener{Listener: listener, slots: make(chan struct{}, limit), done: make(chan struct{})}
}
func (l *limitedListener) Accept() (net.Conn, error) {
	select {
	case <-l.done:
		return nil, net.ErrClosed
	case l.slots <- struct{}{}:
	}
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &limitedConn{Conn: conn, release: func() { <-l.slots }}, nil
}
func (l *limitedListener) Close() error {
	var err error
	l.once.Do(func() { close(l.done); err = l.Listener.Close() })
	return err
}

type limitedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

type idleWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
	timeout    time.Duration
}

func (w *idleWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *idleWriter) deadline()                   { _ = w.controller.SetWriteDeadline(time.Now().Add(w.timeout)) }
func (w *idleWriter) WriteHeader(status int)      { w.deadline(); w.ResponseWriter.WriteHeader(status) }
func (w *idleWriter) Write(data []byte) (int, error) {
	total := 0
	for len(data) > 0 {
		part := data[:min(len(data), 32<<10)]
		w.deadline()
		n, err := w.ResponseWriter.Write(part)
		total += n
		if err != nil {
			return total, err
		}
		if n != len(part) {
			return total, io.ErrShortWrite
		}
		data = data[n:]
	}
	return total, nil
}
func (w *idleWriter) FlushError() error { w.deadline(); return w.controller.Flush() }

func (a *App) limitHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writer := &idleWriter{ResponseWriter: w, controller: http.NewResponseController(w), timeout: writeIdleTimeout}
		writer.deadline()
		// Keep ordinary form POST origins intact. Link landings strip query/path
		// from subsequent Referer headers, even on overload responses.
		policy := "same-origin"
		if r.URL.Path == "/access" {
			policy = "origin"
		}
		writer.Header().Set("Referrer-Policy", policy)
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		select {
		case a.requests <- struct{}{}:
			defer func() { <-a.requests }()
			next.ServeHTTP(writer, r)
		default:
			writer.Header().Set("Retry-After", "5")
			webError(writer, r, "Busy", http.StatusTooManyRequests)
		}
	})
}
