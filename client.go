package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var ID uint32 = 0
var serverStatSlice = []StatPerServer{}
var serverStatSliceLock sync.Mutex

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
func SendOne(sindex int, c net.Conn, seq uint64, payload []byte) (err error) {
	var buf bytes.Buffer
	c.SetWriteDeadline(time.Now().Add(time.Duration(Timeout) * time.Millisecond))
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

func RecvOne(wg *sync.WaitGroup, sindex int, conn net.Conn, seq uint64) (err error) {
	defer wg.Done()
	buf := make([]byte, MaxPktLen+128)

	var n int
	var raddr net.Addr
	var uconn *net.UDPConn
	var tconn *net.TCPConn

	if Tcp {
		tconn = conn.(*net.TCPConn)
		tconn.SetReadDeadline(time.Now().Add(time.Duration(Timeout) * time.Millisecond))
	} else {
		uconn = conn.(*net.UDPConn)
		uconn.SetReadDeadline(time.Now().Add(time.Duration(Timeout) * time.Millisecond))
	}

	if Tcp {
		n, err = tconn.Read(buf)
		raddr = tconn.RemoteAddr()
	} else {
		n, raddr, err = uconn.ReadFromUDP(buf)
	}

	if err != nil {
		fmt.Printf("seq %d: %s\n", ID, err.Error())
		return err
	}
	if n <= 0 {
		fmt.Printf("received zero bytes from %s\n", raddr)
		return fmt.Errorf("received zero bytes from %s", raddr)
	}
	if MutSport && conn != nil {
		conn.Close()
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
			n, raddr.String(), name, resp.Seq, rtt.String())
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
		var maxRttSeq uint64 = 0
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
					maxRttSeq = reqStat.Seq
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
		if s.RespNum > 0 {
			//TotalRtt may overflow
			for _, reqStat := range s.ReqStats {
				if reqStat.RespStatPtr != nil {
					s.AvgRtt += reqStat.RespStatPtr.Rtt/time.Duration(s.RespNum)
				}
			}
			fmt.Printf("successful requests rtt avg/max = %s/%s, max rtt seq is %d\n", s.AvgRtt, s.MaxRtt, maxRttSeq)
		}
		if s.LostNum > 0 {
			fmt.Printf("estimate network failure time: %s\n", time.Duration(s.LostNum) * time.Duration(Interval) * time.Millisecond)
		}
		if len(s.StatPerNames) > 1 {
			fmt.Printf("lb statistics for each rs name:\n")
			for _, stat := range s.StatPerNames {
				respCnt := 0
				maxRtt := time.Duration(0)
				totalRtt := time.Duration(0)
				avgRtt := time.Duration(0)
				var maxRttSeq uint64 = 0
				for _, respStat := range stat.RespStats {
					totalRtt += respStat.Rtt
					if respStat.Rtt > maxRtt {
						maxRtt = respStat.Rtt
						maxRttSeq = respStat.Seq
					}
					respCnt++
				}
				if respCnt > 0 {
					//totalRtt may overflow
					for _, respStat := range stat.RespStats {
						if respStat.Rtt > 0 {
							avgRtt += respStat.Rtt / time.Duration(respCnt)
						}
					}
					fmt.Printf("%s: %d received, rtt avg/max = %s/%s, max rtt seq is %d\n",
							stat.Name, respCnt, avgRtt, maxRtt, maxRttSeq)
				}	
			}
		}
	}
}

func DelayMicroseconds(us int64) {
	var tv syscall.Timeval
	_ = syscall.Gettimeofday(&tv)

	stratTick := int64(tv.Sec)*int64(1000000) + int64(tv.Usec) + us
	endTick := int64(0)
	for endTick < stratTick {
		_ = syscall.Gettimeofday(&tv)
		endTick = int64(tv.Sec)*int64(1000000) + int64(tv.Usec)
	}
}

func delay() {
	if Interval >= 10 {
		time.Sleep(time.Duration(Interval) * time.Millisecond)
	} else if Interval > 2 {
		//use busy wait
		DelayMicroseconds(int64(Interval) * 1000)
	} else {
		// if Interval <= 2, delay less 30us for better accuracy
		DelayMicroseconds(int64(Interval) * 1000 - 30)
	}
}

func SendAndRecvPerServer(wg *sync.WaitGroup, caddr net.IP, sindex int) {
	defer wg.Done()

	var seq uint64 = 0
	var c net.Conn = nil
	var err error
	if !MutSport {
		if !Tcp {
			raddr := &net.UDPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport}
			laddr := &net.UDPAddr{IP: caddr, Port: CPort}
			c, err = Dial("udp", laddr.String(), raddr.String())
			if err != nil {
				fmt.Printf("dial error: %s\n", err)
				return
			}
		} else {
			raddr := &net.TCPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport}
			laddr := &net.TCPAddr{IP: caddr, Port: CPort}
			c, err = Dial("tcp", laddr.String(), raddr.String())
			if err != nil {
				fmt.Printf("dial error: %s\n", err)
				return
			}
			SetTcpConnOptions(c.(*net.TCPConn))
		}
	}
	var payload = make([]byte, PayloadLen-12)
	rand.Read(payload[:])

	var wg1 sync.WaitGroup
	for seq < Count {
		if Interrupted {
			break	
		}
		if seq >= Count {
			break
		}

		if seq != uint64(len(serverStatSlice[sindex].ReqStats)) {
			fmt.Printf("seq %d not equal len(ReqStats) %d\n", seq, len(serverStatSlice[sindex].ReqStats))
			return
		}
		serverStatSlice[sindex].ReqLock.Lock()
		serverStatSlice[sindex].ReqStats = append(serverStatSlice[sindex].ReqStats, ReqStat{
			Seq: seq,
			RespStatPtr: nil,
		})
		serverStatSlice[sindex].ReqLock.Unlock()

		if MutSport {
			// c, err = net.DialUDP("udp", &net.UDPAddr{IP: caddr },
				//   &net.UDPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport})	
	   		if !Tcp {
				raddr := &net.UDPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport}
				laddr := &net.UDPAddr{IP: caddr}
				c, err = Dial("udp", laddr.String(), raddr.String())
				if err != nil {
					fmt.Printf("seq = %d, dial error: %s\n", seq, err)
					seq++
					delay()
					continue
				}
			} else {
				raddr := &net.TCPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport}
				laddr := &net.TCPAddr{IP: caddr}
				c, err = Dial("tcp", laddr.String(), raddr.String())
				if err != nil {
					fmt.Printf("seq = %d, dial error: %s\n", seq, err)
					seq++
					delay()
					continue
				}
				SetTcpConnOptions(c.(*net.TCPConn))
			}
			defer c.Close()
		}

		//send_and_recv()
		err = SendOne(sindex, c, seq, payload)
		if Tcp {
			if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
				if MutSport {
					seq++
					delay()
					continue
				} else {
					fmt.Printf("seq = %d, the only tcp connection we are using is disconnected\n", seq)
					break
				}
			}
		}
		wg1.Add(1)
		go RecvOne(&wg1, sindex, c, seq)
		seq++
		delay()
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
	if MutSport {
		ID = ID | 0x00000001 	
	} else {
		ID = ID & 0xfffffffe
	}
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
