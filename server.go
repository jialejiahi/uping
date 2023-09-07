package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func RecvAndSend(wg *sync.WaitGroup, conn *net.UDPConn) {
	defer wg.Done()
	for !Interrupted {
		buf := make([]byte, MaxPktLen+128)
		n, raddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("read error:", err)
			continue
		}
		if n <= 0 {
			continue
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
		if Dbglvl > 1 {
			fmt.Printf("Req is %v\n", req)
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
		binary.Write(&rbuf, binary.BigEndian, buf[16+resp.NameLen:n])
		rbufBytes := rbuf.Bytes()
		if Dbglvl > 1 {
			for i := 0; i < 64; i++ {
				fmt.Printf("%02x ", rbufBytes[i])
			}
			fmt.Println("")
		}

		n, err = conn.WriteToUDP(rbuf.Bytes(), raddr)
		if err != nil {
			fmt.Println("write error:", err)
			continue
		}
		if Dbglvl > 1 {
			fmt.Printf("Send %d bytes to %s\n", n, raddr.String())
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
	for _, port := range plist {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: saddr, Port: int(port)})
		if err != nil {
			fmt.Println("listen error:", err)
			return
		}
		conns = append(conns, conn)
		defer conn.Close()
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
	for _, conn := range conns {
		wg.Add(1)
		go RecvAndSend(&wg, conn)
	}
	wg.Wait()

}
