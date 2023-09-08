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

	"golang.org/x/sys/unix"
)

var ID uint32 = 0
var serverStatSlice = []StatPerServer{}
var serverStatSliceLock sync.Mutex

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
// use a slice, struct in map can not be changed
func get_stat_for_name(server_index int, name string) int {
	serverStatSliceLock.Lock()
	defer serverStatSliceLock.Unlock()
	for i, v := range serverStatSlice[server_index].StatPerNames {
		if v.Name == name {
			return i
		}
	}
	serverStatSlice[server_index].StatPerNames = append(serverStatSlice[server_index].StatPerNames, StatPerName{
		Name: name,
		RespStats: []RespStat{},
		RespLock: &sync.RWMutex{},
	})
	return len(serverStatSlice[server_index].StatPerNames) - 1
}

//construct and send one packet
func SendOne(wg *sync.WaitGroup, sindex int, c *net.UDPConn, seq uint64, payload []byte) (err error) {
	defer wg.Done()

	var buf bytes.Buffer
	req := ReqHeader {
		Id: ID,
		Seq: seq,
	}
	binary.Write(&buf, binary.BigEndian, &req)
	binary.Write(&buf, binary.BigEndian, payload[:])

	n, err := c.Write(buf.Bytes());
	if err != nil {
		fmt.Println(err)
		return err
	}
	timeStamp := time.Now()
	serverStatSlice[sindex].ReqLock.Lock()
	serverStatSlice[sindex].ReqStats[seq].TimeStamp = timeStamp
	serverStatSlice[sindex].ReqLock.Unlock()
	if Dbglvl > 1 {
		fmt.Printf("Sent %d bytes to %s: seq=%d timestamp=%s\n",
			n, c.RemoteAddr().String(), seq, timeStamp.Format(time.StampMilli))
	}

	return nil
}

func RecvOne(wg *sync.WaitGroup, sindex int, conn *net.UDPConn) error {
	defer wg.Done()

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
		fmt.Printf("Resp header is %v\n", resp)
	}
	name := string(buf[16:16+resp.NameLen])

	//sequence is index
	rtt := time.Since(serverStatSlice[sindex].ReqStats[resp.Seq].TimeStamp)
	respStat := RespStat {
		Seq: resp.Seq,
		TimeStamp: time.Now(),
		Rtt: rtt,
	}
	i := get_stat_for_name(sindex, name)
	serverStatSlice[sindex].StatPerNames[i].RespLock.Lock()
	serverStatSlice[sindex].StatPerNames[i].RespStats = append(serverStatSlice[sindex].StatPerNames[i].RespStats, respStat)
	serverStatSlice[sindex].StatPerNames[i].RespLock.Unlock()

	serverStatSlice[sindex].ReqLock.Lock()
	serverStatSlice[sindex].ReqStats[resp.Seq].RespStatPtr = &respStat
	serverStatSlice[sindex].ReqLock.Unlock()

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

	for _, s := range serverStatSlice {
		if len(s.StatPerNames) > 1 {
			fmt.Printf("\n--- %s:%d uping statistics ---\n", s.Saddr, s.Sport)
		} else if len(s.StatPerNames) == 1 {
			fmt.Printf("\n--- %s:%d(%s) uping statistics ---\n", s.Saddr, s.Sport, s.StatPerNames[0].Name)
		} else {
			fmt.Printf("\n--- %s:%d uping statistics ---\n", s.Saddr, s.Sport)
			fmt.Printf("0 received, 100%% packet loss\n")
			continue
		}
		for i, reqStat := range s.ReqStats {
			if i != int(reqStat.Seq) {
				fmt.Printf("seq=%d not equal slice index %d", reqStat.Seq, i)	
			}
			if reqStat.RespStatPtr != nil {
				if Dbglvl > 1 {
					fmt.Printf("seq=%d time=%s\n", reqStat.Seq, reqStat.RespStatPtr.Rtt.String())
				}
				s.TotalRtt += reqStat.RespStatPtr.Rtt
				if reqStat.RespStatPtr.Rtt > s.MaxRtt {
					s.MaxRtt = reqStat.RespStatPtr.Rtt
				}
				s.RespNum++
			} else {
				if Dbglvl > 1 {
					fmt.Printf("seq=%d packet lost\n", reqStat.Seq)
				}
				s.LostNum++
			}
		}
		fmt.Printf("%d packets transmitted, %d received, %d packet loss\n", len(s.ReqStats), s.RespNum, s.LostNum)
		fmt.Printf("successful requests rtt avg/max = %s/%s\n", s.TotalRtt/time.Duration(s.RespNum), s.MaxRtt)
		if s.LostNum > 0 {
			fmt.Printf("estimate network failure time: %s\n", time.Duration(s.LostNum) * time.Duration(Interval) * time.Millisecond)
		}
		if len(s.StatPerNames) > 1 {
			fmt.Printf("lb statistics for each rs name:\n")
			for _, stat := range s.StatPerNames {
				respCnt := 0
				maxRtt := time.Duration(0)
				totalRtt := time.Duration(0)
				for _, respStat := range stat.RespStats {
					totalRtt += respStat.Rtt
					if respStat.Rtt > maxRtt {
						maxRtt = respStat.Rtt
					}
					respCnt++
				}
				if respCnt > 0 {
					fmt.Printf("%s: %d received, rtt avg/max = %s/%s\n",
							stat.Name, respCnt, totalRtt/time.Duration(respCnt), maxRtt)
				}	
			}
		}
	}
}

func SendAndRecvPerServer(wg *sync.WaitGroup, caddr net.IP, sindex int) {
	defer wg.Done()

	var seq uint64 = 0
	var err error = nil
	var c *net.UDPConn = nil
	if !MutSport {
		d := net.Dialer{
			Control: Control,
			LocalAddr: &net.UDPAddr{IP: caddr, Port: CPort},
		}
		raddr := &net.UDPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport}
		cc, err := d.Dial("udp", raddr.String())
		//c, err = net.DialUDP("udp", &net.UDPAddr{IP: caddr, Port: CPort},
		//	  &net.UDPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport})
		//c, err = net.DialUDP("udp", &net.UDPAddr{IP: caddr, Port: CPort},
		//	  &net.UDPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport})	
		if err != nil {
			fmt.Printf("dial error: %s\n", err)
			return
		}
		c = cc.(*net.UDPConn)
		defer c.Close()
	}
	var payload = make([]byte, PayloadLen-12)
	rand.Read(payload[:])

	wg1 := sync.WaitGroup{}
	for seq < Count {
		if Interrupted {
			break	
		}
		if seq >= Count {
			break
		}

		serverStatSlice[sindex].ReqLock.Lock()
		if seq != uint64(len(serverStatSlice[sindex].ReqStats)) {
			fmt.Printf("seq %d not equal len(ReqStats) %d\n", seq, len(serverStatSlice[sindex].ReqStats))
			return
		}
		serverStatSlice[sindex].ReqStats = append(serverStatSlice[sindex].ReqStats, ReqStat{
			Seq: seq,
			RespStatPtr: nil,
		})
		serverStatSlice[sindex].ReqLock.Unlock()
		if MutSport {
			c, err = net.DialUDP("udp", &net.UDPAddr{IP: caddr },
				  &net.UDPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport})	
			if err != nil {
				fmt.Printf("dial error: %s\n", err)
				return
			}
			defer c.Close()

			wg1.Add(1)
			go SendOne(&wg1, sindex, c, seq, payload)
			wg1.Add(1)
			go RecvOne(&wg1, sindex, c)
		} else {
			wg1.Add(1)
			go SendOne(&wg1, sindex, c, seq, payload)
			wg1.Add(1)
			go RecvOne(&wg1, sindex, c)
		}

		time.Sleep(time.Duration(Interval) * time.Millisecond)
		seq++
	}
	wg1.Wait()
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
	ID = rand.Uint32()	
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


	var wg sync.WaitGroup
	for _, p := range plist {
		serverStat := StatPerServer{
			Saddr: saddr,
			Sport: int(p),
			ReqNum: 0,
			RespNum: 0,
			LostNum: 0,
			TotalRtt: time.Duration(0),
			MaxRtt: time.Duration(0),
			AvgRtt: time.Duration(0),
			ReqStats: []ReqStat{},
			ReqLock: &sync.RWMutex{},
		}
		serverStatSlice = append(serverStatSlice, serverStat)
		sindex := len(serverStatSlice) - 1

		wg.Add(1)
		go SendAndRecvPerServer(&wg, caddr, sindex)
	}
	wg.Wait()
	get_all_stats()

}
