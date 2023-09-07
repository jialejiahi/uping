package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var stat Stat
var statServerSlice = []StatPerServer{}
var statServerSliceLock sync.Mutex


// use a slice, struct in map can not be changed
func get_stat_for_server(addr string, name string) int {
	statServerSliceLock.Lock()
	defer statServerSliceLock.Unlock()
	for i, v := range statServerSlice {
		if v.Name == name && v.Addr == addr {
			return i
		}
	}
	statServerSlice = append(statServerSlice, StatPerServer{
		Addr: addr,
		Name: name,
		RespStats: []*RespStat{},
		RespLock: &sync.RWMutex{},
	})
	return len(statServerSlice) - 1
}

func SendOne(conn *net.UDPConn, seq uint64, payload []byte) error {
//construct and send one packet
	var buf bytes.Buffer
	req := ReqHeader {
		Id: stat.Id,
		Seq: seq,
	}
	binary.Write(&buf, binary.BigEndian, &req)
	binary.Write(&buf, binary.BigEndian, payload[:])

	n, err := conn.Write(buf.Bytes());
	if err != nil {
		fmt.Println(err)
		return err
	}
	timeStamp := time.Now()
	if Dbglvl > 1 {
		fmt.Printf("Sent %d bytes to %s: seq=%d timestamp=%s\n",
			n, conn.RemoteAddr().String(), seq, timeStamp.Format(time.StampMilli))
	}
	stat.ReqLock.Lock()
	stat.ReqStats[seq].TimeStamp = timeStamp
	stat.ReqLock.Unlock()

	return nil
}

func RecvOne(conn *net.UDPConn) error {
	buf := make([]byte, MaxPktLen+128)
	conn.SetReadDeadline(time.Now().Add(time.Duration(Timeout) * time.Millisecond))
	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		fmt.Println(err)
		return err
	}
	if n <= 0 {
		fmt.Printf("received zero bytes from %s\n", addr)
		return fmt.Errorf("received zero bytes from %s", addr)
	}
	resp := RespHeader{
		Id:	  binary.BigEndian.Uint32(buf[0:4]),
		Seq:   binary.BigEndian.Uint64(buf[4:12]),
		NameLen: binary.BigEndian.Uint32(buf[12:16]),
	}

	if Dbglvl > 1 {
		fmt.Printf("Resp is %v\n", resp)
	}
	name := string(buf[16:16+resp.NameLen])

	//sequence is index
	rtt := time.Since(stat.ReqStats[resp.Seq].TimeStamp)
	respStat := RespStat {
		Seq: resp.Seq,
		TimeStamp: time.Now(),
		Rtt: rtt,
	}
	i := get_stat_for_server(addr.String(), name)
	statServerSlice[i].RespLock.Lock()
	statServerSlice[i].RespStats = append(statServerSlice[i].RespStats, &respStat)
	statServerSlice[i].RespLock.Unlock()

	stat.ReqLock.Lock()
	stat.ReqStats[resp.Seq].RespStatPtr = &respStat
	stat.ReqLock.Unlock()

	if Dbglvl > 0 {
		fmt.Printf("%d bytes from %s(%s): seq=%d time=%s\n",
			n, addr.String(), name, resp.Seq, rtt.String())
	}

	return nil
}
//--- 127.0.0.1 ping statistics ---
//10 packets transmitted, 10 received, 0% packet loss, time 902ms
//rtt min/avg/max/mdev = 0.015/0.020/0.026/0.006 ms
func get_all_stats() {

	fmt.Println()
	fmt.Println("--- uping statistics ---")
	maxRtt := time.Duration(0)
	totalRtt := time.Duration(0)
	respCnt := 0
	lostCnt := 0
	for i, reqStat := range stat.ReqStats {
		if i != int(reqStat.Seq) {
			fmt.Printf("seq=%d not equal slice index %d", reqStat.Seq, i)	
		}
		if reqStat.RespStatPtr != nil {
			if Dbglvl > 1 {
				fmt.Printf("seq=%d time=%s\n", reqStat.Seq, reqStat.RespStatPtr.Rtt.String())
			}
			totalRtt += reqStat.RespStatPtr.Rtt
			if reqStat.RespStatPtr.Rtt > maxRtt {
				maxRtt = reqStat.RespStatPtr.Rtt
			}
			respCnt++
		} else {
			if Dbglvl > 1 {
				fmt.Printf("seq=%d packet lost\n", reqStat.Seq)
			}
			lostCnt++
		}
	}
	fmt.Printf("%d packets transmitted, %d received, %d packet loss\n", len(stat.ReqStats), respCnt, lostCnt)
	fmt.Printf("rtt avg/max = %s/%s\n", totalRtt/time.Duration(respCnt), maxRtt)

	for _, serverStat := range statServerSlice {
	    respCnt := 0
		maxRtt := time.Duration(0)
		totalRtt := time.Duration(0)
		for _, respStat := range serverStat.RespStats {
			totalRtt += respStat.Rtt
			if respStat.Rtt > maxRtt {
				maxRtt = respStat.Rtt
			}
			respCnt++
		}
		if respCnt > 0 {
			fmt.Printf("%s(%s) %d received, rtt avg/max = %s/%s\n",
					serverStat.Addr, serverStat.Name, respCnt, totalRtt/time.Duration(respCnt), maxRtt)
		}
	}
}

func SendAndRecv(conns []*net.UDPConn) {

	var payload = make([]byte, PayloadLen-12)
	rand.Read(payload[:])

	var seq uint64 = 0
	for seq < Count {
		for _, c := range conns {
			if Interrupted {
				break	
			}
			if seq >= Count {
				break
			}

			stat.ReqLock.Lock()
			stat.ReqStats = append(stat.ReqStats, ReqStat{
				Seq: seq,
				RespStatPtr: nil,
			})
			stat.ReqLock.Unlock()

			go SendOne(c, seq, payload)
			go RecvOne(c)
			time.Sleep(time.Duration(Interval) * time.Millisecond)
			seq++
		}
	}
	get_all_stats()
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
	if Dbglvl > 1 {
		fmt.Printf("Params Local Addr %s:%d\n", CAddr, CPort)
		fmt.Printf("Params Remote Addr %s:%s\n", SAddr, SPortList)
		fmt.Printf("Params Payload Len:%d, Inteval:%d, Count:%d, Timeout:%d\n",
			PayloadLen, Interval, Count, Timeout)
	}
	stat.Id = rand.Uint32()	
	stat.ReqLock = &sync.RWMutex{}
	//handle
	Interrupted = false
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT)

	go func() {
		<-c
		//fmt.Printf("Program Interrupted, Clean up and Exit\n")
		//1. stop sending
		//2. stop receiving
		Interrupted = true
		//3. close listener
		//4. calcuate statistics
		time.Sleep(time.Millisecond)
		get_all_stats()
		os.Exit(0)
	}()


	conns := []*net.UDPConn{}
	for _, p := range plist {
		c, err := net.DialUDP("udp", &net.UDPAddr{IP: caddr, Port: CPort}, &net.UDPAddr{IP: saddr, Port: int(p)})	
		if err != nil {
			fmt.Printf("dial error: %s\n", err)
			return
		}
		conns = append(conns, c)
		defer c.Close()
	}
	//var wg sync.WaitGroup
	//for _, c := range conns {
	//	wg.Add(1)
	//	go Recv(&wg, c)
	//}

	//wg.Add(1)
	SendAndRecv(conns)

	//wg.Wait()
}
