package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func RecvAndSendOne(c net.Conn, buf []byte) (mutable bool, err error) {

	var n int
	var raddr net.Addr

	var uconn *net.UDPConn
	var tconn *net.TCPConn

	if Tcp {
		tconn = c.(*net.TCPConn)
	} else {
		uconn = c.(*net.UDPConn)
	}

	if Tcp {
		//TODO: handle receive fragments
		n, err = tconn.Read(buf)
		raddr = tconn.RemoteAddr()
	} else {
		n, raddr, err = uconn.ReadFromUDP(buf)
	}
	if err != nil {
		fmt.Println("read error:", err)
		return
	}
	if n < 12 {
		if Dbglvl > 1 {
			fmt.Printf("invalid request of length %d\n", n)
		}
		return
	}
	if Dbglvl > 1 {
		fmt.Printf("%d bytes from %s\n", n, raddr.String())
	}

	if Dbglvl > 1 {
		for i := 0; i < 64; i++ {
			fmt.Printf("%02x ", buf[i])
		}
		fmt.Println("")
	}
	//parse request
	req := ReqHeader{
	    Id:	  binary.BigEndian.Uint32(buf[0:4]),
		Seq:   binary.BigEndian.Uint64(buf[4:12]),
	}
	if Check {
		if req.Id & 0xffff0000 != 0x55aa0000 {
			return
		}
	}
	if req.Id & 0x00000001 != 0 {
		//mutable
		mutable = true
	} else {
		mutable = false
	}
	if Dbglvl > 1 {
		fmt.Printf("Req is %v, mutable bit is %v\n", req, mutable)
	}
	//make a reply
	resp := RespHeader{
		Id: req.Id,
		Seq: req.Seq,
		NameLen: uint32(len(Name)),
	}

	if Dbglvl > 1 {
		fmt.Printf("Resp is %v\n", resp)
	}
	var rbuf bytes.Buffer
	binary.Write(&rbuf, binary.BigEndian, &resp)
	binary.Write(&rbuf, binary.BigEndian, []byte(Name))
	if (n > int(16 + resp.NameLen)) {
		binary.Write(&rbuf, binary.BigEndian, buf[16+resp.NameLen:n])
	}
	rbufBytes := rbuf.Bytes()
	if Dbglvl > 1 {
		for i := 0; i < len(rbufBytes) && i < 64; i++ {
			fmt.Printf("%02x ", rbufBytes[i])
		}
		fmt.Println("")
	}

	if Tcp {
		n, err = tconn.Write(rbuf.Bytes())	
	} else {
		n, err = uconn.WriteToUDP(rbuf.Bytes(), raddr.(*net.UDPAddr))
	}
	if err != nil {
		fmt.Println("write error:", err)
		return
	}
	if Dbglvl > 1 {
		fmt.Printf("Send %d bytes to %s\n", n, raddr.String())
	}
	return
}

func HandleTcpLis(wg *sync.WaitGroup, lis *net.TCPListener) {
	defer wg.Done()
	var wg1 sync.WaitGroup

	for !Interrupted {
		conn, err := lis.AcceptTCP()
		if err != nil {
			fmt.Println("accept error:", err)
			continue
		}
		if Dbglvl > 1 {
			fmt.Printf("Accept a connection from %s\n", conn.RemoteAddr().String())
		}
		SetTcpConnOptions(conn)
		buf := make([]byte, MaxPktLen+128)
		MutCT, e := RecvAndSendOne(conn, buf)
		if e != nil {
			conn.Close()	
			continue
		}
		if MutCT {
			conn.Close()	
		} else {
			wg1.Add(1)
			go RecvAndSendAll(&wg1, conn)
		}
	}
	wg1.Wait()
}

func RecvAndSendAll(wg *sync.WaitGroup, c net.Conn) {
	defer wg.Done()

	buf := make([]byte, MaxPktLen+128)

	for !Interrupted {
		_, err := RecvAndSendOne(c, buf)
		if Tcp {
			if errors.Is(err, syscall.EPIPE) || errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) {
				return
			}
		}
	}
}
// server params
// Dbglvl: debug level
// SAddr: server address -> saddr
// SPortList: server port list -> plist
// Name: server name
func server_main(saddr net.IP, plist []uint16) {
	// create server socket
	if Name == "" {
		Name, _ = os.Hostname()
	}
	if len(Name) > 48 {
		Name = Name[:48]	
		fmt.Printf("Server name too long, truncated to %s\n", Name)
	}
	if Dbglvl > 0 {
		fmt.Printf("listening %s:%s, response with name %s\n", SAddr, SPortList, Name)
	}

	//listen control socket
	conns := []*net.UDPConn{}
	tlisteners := []*net.TCPListener{}
	for _, port := range plist {
		if !Tcp {
			conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: saddr, Port: int(port)})
			if err != nil {
				fmt.Println("listen error:", err)
				return
			}
			conns = append(conns, conn)
			defer conn.Close()
		} else {
			addr := &net.TCPAddr{IP: saddr, Port: int(port)}
			lis, err := Listen("tcp", addr.String())
			if err != nil {
				fmt.Println("listen error:", err)
				return
			}

			tlisteners = append(tlisteners, lis.(*net.TCPListener))
		}
	}
	//handle
	Interrupted = false
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT)

	go func() {
		<-c
		fmt.Printf("Program Interrupted, Clean up and Exit\n")
		//1. stop sending
		//2. stop receiving
		Interrupted = true
		//3. close listener
		//4. calcuate statistics
		os.Exit(0)
	}()
	var wg sync.WaitGroup
	// receive and response
	if !Tcp {
		for _, conn := range conns {
			wg.Add(1)
			go RecvAndSendAll(&wg, conn)
		}
	} else {
		for _, lis := range tlisteners {
			wg.Add(1)
			go HandleTcpLis(&wg, lis)
		}
	}
	wg.Wait()

}
