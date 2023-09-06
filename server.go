package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func RecvAndSend(wg *sync.WaitGroup, conn net.Conn) {
	defer wg.Done()
	for !Interrupted {
		buf := make([]byte, MaxPktLen+128)
		n, err := conn.Read(buf)
		if err != nil {
			fmt.Println("read error:", err)
			return
		}
		//handle one request parse recevied data and reply
		go func(n int, buf []byte) {
			if n > 0 {
				fmt.Printf("recv: %s\n", buf[:n])
			}
			if n == 0 {
				return
			}
			//parse request
			//make a reply
			conn.Write(buf[:n])
		}(n, buf)
	}
}

// server params
// Dbglvl: debug level
// SAddr: server address -> saddr
// SPortList: server port list -> plist
// Name: server name
func server_main(saddr net.IP, plist []uint16) {
	// create server socket
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
		RecvAndSend(&wg, conn)
	}
	wg.Wait()

}
