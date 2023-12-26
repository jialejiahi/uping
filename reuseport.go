package main

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func ResolveAddr(network, address string) (net.Addr, error) {
	switch network {
	default:
		return nil, net.UnknownNetworkError(network)
	case "ip", "ip4", "ip6":
		return net.ResolveIPAddr(network, address)
	case "tcp", "tcp4", "tcp6":
		return net.ResolveTCPAddr(network, address)
	case "udp", "udp4", "udp6":
		return net.ResolveUDPAddr(network, address)
	case "unix", "unixgram", "unixpacket":
		return net.ResolveUnixAddr(network, address)
	}
}

func SetTcpConnOptions(c *net.TCPConn) error {
	var err error


	c.SetNoDelay(true)
	c.SetLinger(0)

	f, _ := c.File()
	defer f.Close()

	err = unix.SetsockoptInt(int(f.Fd()), unix.SOL_TCP, unix.TCP_CORK, 0)
	if err != nil {
		return err
	}
	err = unix.SetsockoptInt(int(f.Fd()), unix.SOL_TCP, unix.TCP_FASTOPEN, 0)
	if err != nil {
		return err
	}
	err = unix.SetsockoptInt(int(f.Fd()), unix.SOL_TCP, unix.TCP_QUICKACK, 0)
	if err != nil {
		return err
	}
	return err
}

func Control(network, address string, c syscall.RawConn) error {
	var err error
	c.Control(func(fd uintptr) {
		err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		if err != nil {
			return
		}

		err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		if err != nil {
			return
		}
	})
	return err
}

var listenConfig = net.ListenConfig{
	Control: Control,
}

// Listen listens at the given network and address. see net.Listen
// Returns a net.Listener created from a file discriptor for a socket
// with SO_REUSEPORT and SO_REUSEADDR option set.
func Listen(network, address string) (net.Listener, error) {
	return listenConfig.Listen(context.Background(), network, address)
}

// ListenPacket listens at the given network and address. see net.ListenPacket
// Returns a net.Listener created from a file discriptor for a socket
// with SO_REUSEPORT and SO_REUSEADDR option set.
func ListenPacket(network, address string) (net.PacketConn, error) {
	return listenConfig.ListenPacket(context.Background(), network, address)
}

// Dial dials the given network and address. see net.Dialer.Dial
// Returns a net.Conn created from a file descriptor for a socket
// with SO_REUSEPORT and SO_REUSEADDR option set.
func Dial(network, laddr, raddr string, timeout time.Duration) (net.Conn, error) {
	//timeout in ms
	nla, err := ResolveAddr(network, laddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve local addr: %w", err)
	}
	d := net.Dialer{
		Control:   Control,
		LocalAddr: nla,
		Timeout:   timeout * time.Millisecond,
	}
	return d.Dial(network, raddr)
}
