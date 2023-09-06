package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func Send(wg *sync.WaitGroup, conn net.Conn) {
	defer wg.Done()
	for !Interrupted {
		_, err := conn.Write([]byte("Hello, world!\n"))
		if err != nil {
			fmt.Println(err)
			break
		}
	}
}
func Recv(wg *sync.WaitGroup, conn net.Conn) {
	defer wg.Done()
	for !Interrupted {
		buf := make([]byte, MaxPktLen+128)
		n, err := conn.Read(buf)
		if err != nil {
			fmt.Println(err)
			break
		}
		fmt.Printf("Received: %s\n", buf[:n])
	}
}

// client params
// Dbglvl: debug level
// SAddr: server address -> saddr
// CAddr: server address -> caddr
// SPortList: server port list -> plist
// CPort: client port
// PayloadLen: payload length
// Interval: interval
// Count: count
// Timeout: timeout
func client_main(saddr net.IP, caddr net.IP, plist []uint16) {
	// create server socket
	if Dbglvl > 0 {
		fmt.Printf("listening %s:%d\n", CAddr, CPort)
	}
	if Dbglvl > 1 {
		fmt.Printf("Params Saddr:%s, Sport:%s\n", SAddr, SPortList)
		fmt.Printf("Params Len:%d, Inteval:%d, Count:%d, Timeout:%d\n",
			PayloadLen, Interval, Count, Timeout)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: saddr, Port: CPort})
	if err != nil {
		fmt.Printf("listen error: %s\n", err)
		return
	}
	defer conn.Close()
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

	wg.Add(1)
	go Send(&wg, conn)

	wg.Add(1)
	go Recv(&wg, conn)

	wg.Wait()
}
