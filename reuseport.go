package main

import (
	"context"
	"crypto/tls"
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

func SetTcpConnQuickAck(c *net.TCPConn) error {
	rc, _ := c.SyscallConn()
	rc.Control(func(fd uintptr) {
		if EnableQuickAck {
			unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_QUICKACK, 1)
		} else {
			unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_QUICKACK, 0)
		}
	})
	return nil
}

func SetTcpConnOptions(c *net.TCPConn) error {

	//决定了何时发送Payload，开启了则write时会攒包，到MSS或者收到ACK后才发
	//因为正常情况下uping探测是1发1收，所以不会出现两个连续的发送，造成攒包，这个参数通常无影响
	//除非server->client方向有超时，才会造成攒包，这样丢包就更严重了，所以这个参数一般不用
	c.SetNoDelay(NoDelay)
	//是否使用rst断开连接
	c.SetLinger(0)

	rc, _ := c.SyscallConn()
	rc.Control(func(fd uintptr) {
	    //决定了是否等待数据完整了再发送, 设置为1的话，一定会攒包，影响小包探测了
		unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_CORK, 0)
		//if DisableNoDelay {
		//	unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_CORK, 0)
		//} else {
		//	unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_CORK, 1)
		//}
	    //决定了是否可以在syn中携带数据，如果要携带，程序中还需要指定cookie，客户端还有个TCP_FASTOPEN_CONNECT选项
	    //这个设置当前其实没用
		//unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_FASTOPEN, 0)
	    // 决定了合适发送Ack， 开启了发送ack可能延迟，延迟到有数据发送或者定时器超时
		//unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_QUICKACK, 0)
		// 早设置一遍QuickAck
		//if EnableQuickAck {
		//	unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_QUICKACK, 1)
		//} else {
		//	unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_QUICKACK, 0)
		//}
	})

	return nil
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
		if Tcp {
		    if EnableQuickAck {
		        unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_QUICKACK, 1)
		    } else {
		        unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_QUICKACK, 0)
		    }
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
// CustomTLSConn wraps a tls.Conn and the original net.Conn
type CustomTLSConn struct {
	*tls.Conn          // Embed the tls.Conn
	originalConn net.Conn // Store the original net.Conn
}

// GetOriginalConn returns the original net.Conn
func (c CustomTLSConn) GetOriginalConn() net.Conn {
	return c.originalConn
}

// Dial dials the given network and address. see net.Dialer.Dial
// Returns a net.Conn created from a file descriptor for a socket
// with SO_REUSEPORT and SO_REUSEADDR option set.

func Dial(network, laddr, raddr string, timeout time.Duration, ssl bool) (net.Conn, error) {
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
	if ssl {
	    tlsConfig := &tls.Config{
            InsecureSkipVerify: true, // Set to true for testing purposes, but use proper certificate verification in production
	    }
		rawConn, err := d.Dial(network, raddr)
        if err != nil {
            return nil, err
        }
		// Upgrade the connection to TLS
        tlsConn := tls.Client(rawConn, tlsConfig)

        // Perform the TLS handshake
        err = tlsConn.Handshake()
        if err != nil {
            rawConn.Close()
            return nil, err
        }

	    return CustomTLSConn {
		   Conn:         tlsConn,
		   originalConn: rawConn,
	    }, nil
	} else {
		rawConn, err := d.Dial(network, raddr)
        if err != nil {
            return nil, err
        }
	    return  rawConn, nil
	}
}
