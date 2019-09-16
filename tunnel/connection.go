package tunnel

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	adapters "github.com/zu1k/clashr/adapters/inbound"
	"github.com/zu1k/clashr/common/pool"
)

func (t *Tunnel) handleHTTP(request *adapters.HTTPAdapter, outbound net.Conn) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("panic recover")
		}
	}()
	conn := newTrafficTrack(outbound, t.traffic)
	req := request.R
	host := req.Host

	inboundReeder := bufio.NewReader(request)
	outboundReeder := bufio.NewReader(conn)

	for {
		keepAlive := strings.TrimSpace(strings.ToLower(req.Header.Get("Proxy-Connection"))) == "keep-alive"

		req.Header.Set("Connection", "close")
		req.RequestURI = ""
		adapters.RemoveHopByHopHeaders(req.Header)

		err := req.Write(conn)
		if err != nil {
			break
		}

	handleResponse:
		resp, err := http.ReadResponse(outboundReeder, req)
		if err != nil {
			break
		}
		adapters.RemoveHopByHopHeaders(resp.Header)

		if resp.StatusCode == http.StatusContinue {
			err = resp.Write(request)
			if err != nil {
				break
			}
			goto handleResponse
		}

		if keepAlive || resp.ContentLength >= 0 {
			resp.Header.Set("Proxy-Connection", "keep-alive")
			resp.Header.Set("Connection", "keep-alive")
			resp.Header.Set("Keep-Alive", "timeout=4")
			resp.Close = false
		} else {
			resp.Close = true
		}
		err = resp.Write(request)
		if err != nil || resp.Close {
			break
		}

		req, err = http.ReadRequest(inboundReeder)
		if err != nil {
			break
		}

		// Sometimes firefox just open a socket to process multiple domains in HTTP
		// The temporary solution is close connection when encountering different HOST
		if req.Host != host {
			break
		}
	}
}

func (t *Tunnel) handleUDPToRemote(conn net.Conn, pc net.PacketConn, addr net.Addr) {
	buf := pool.BufPool.Get().([]byte)
	defer pool.BufPool.Put(buf[:cap(buf)])

	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	if _, err = pc.WriteTo(buf[:n], addr); err != nil {
		return
	}
	t.traffic.Up() <- int64(n)
}

func (t *Tunnel) handleUDPToLocal(conn net.Conn, pc net.PacketConn) {
	buf := pool.BufPool.Get().([]byte)
	defer pool.BufPool.Put(buf[:cap(buf)])

	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}

		n, err = conn.Write(buf[:n])
		if err != nil {
			return
		}
		t.traffic.Down() <- int64(n)
	}
}

func (t *Tunnel) handleSocket(request *adapters.SocketAdapter, outbound net.Conn) {
	conn := newTrafficTrack(outbound, t.traffic)
	relay(request, conn)
}

// relay copies between left and right bidirectionally.
func relay(leftConn, rightConn net.Conn) {
	ch := make(chan error)

	go func() {
		buf := pool.BufPool.Get().([]byte)
		_, err := io.CopyBuffer(leftConn, rightConn, buf)
		pool.BufPool.Put(buf)
		_ = leftConn.SetReadDeadline(time.Now())
		ch <- err
	}()

	buf := pool.BufPool.Get().([]byte)
	_, _ = io.CopyBuffer(rightConn, leftConn, buf)
	pool.BufPool.Put(buf[:cap(buf)])
	_ = rightConn.SetReadDeadline(time.Now())
	<-ch
}
