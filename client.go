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
func SendOne(sindex int, c net.Conn, seq uint64, noWriteFlag bool, connectTime time.Time, payload []byte) (err error) {
	var buf bytes.Buffer
	var n int
	req := ReqHeader {
		Id: ID,
		Seq: seq,
	}
	binary.Write(&buf, binary.BigEndian, &req)
	if StrictCheck {
		for i := 0; i < len(payload); i++ {
			payload[i] = byte(seq%256)
		}
	}
	binary.Write(&buf, binary.BigEndian, payload[:])

	if !noWriteFlag {
		//if Tcp {
		//	var tconn *net.TCPConn
		//    if Ssl {
		//	    tconn = c.(CustomTLSConn).GetOriginalConn().(*net.TCPConn)
		//	} else {
		//		tconn = c.(*net.TCPConn)
		//	}
		//	SetTcpConnQuickAck(tconn)
		//}

		c.SetWriteDeadline(time.Now().Add(time.Duration(Timeout) * time.Millisecond))
		n, err = c.Write(buf.Bytes());
		if err != nil {
			fmt.Println(err)
			return
		}
	} else {
		n = 0	
		err = nil
	}
	timeStamp := time.Now()
	if MutCT && Tcp {
		timeStamp = connectTime
	}
	if seq >= uint64(len(serverStatSlice[sindex].ReqStats)) {
		fmt.Printf("send seq out of range: seq=%d, len=%d, server index=%d, remote=%v\n",
			seq, len(serverStatSlice[sindex].ReqStats), sindex, c.RemoteAddr())
		err = &SendSeqError
		return
	}
	serverStatSlice[sindex].ReqLock.Lock()
	serverStatSlice[sindex].ReqStats[seq].TimeStamp = timeStamp
	serverStatSlice[sindex].ReqLock.Unlock()
	if Dbglvl > 1 && !noWriteFlag {
		fmt.Printf("Sent %d bytes to %s: seq=%d timestamp=%s\n",
			n, c.RemoteAddr().String(), seq, timeStamp.Format(time.StampMilli))
	}

	return nil
}

//TODO: handle tcp recv combined packet(two payload in one packet)
func RecvOne(wg *sync.WaitGroup, sindex int, conn net.Conn, seq uint64) (err error) {
	defer wg.Done()
	buf := make([]byte, MaxPktLen+128)

	var n int
	var raddr net.Addr
	var uconn *net.UDPConn
	var tconn *net.TCPConn

	if Tcp {
	    if Ssl {
		    tconn = conn.(CustomTLSConn).GetOriginalConn().(*net.TCPConn)
		} else {
		    tconn = conn.(*net.TCPConn)
		}
		SetTcpConnQuickAck(tconn)
		tconn.SetReadDeadline(time.Now().Add(time.Duration(Timeout) * time.Millisecond))
	} else {
		uconn = conn.(*net.UDPConn)
		uconn.SetReadDeadline(time.Now().Add(time.Duration(Timeout) * time.Millisecond))
	}

	if Tcp {
	    if Ssl {
			n, err = conn.Read(buf)
		} else {
			n, err = tconn.Read(buf)
		}
		raddr = tconn.RemoteAddr()
	} else {
		n, raddr, err = uconn.ReadFromUDP(buf)
	}

	if err != nil {
		fmt.Printf("read error, last sent seq is %d: %s\n", seq, err.Error())
		return err
	}
	if n <= 0 {
		fmt.Printf("received zero bytes from %s\n", raddr)
		return fmt.Errorf("received zero bytes from %s", raddr)
	}
	if MutCT && conn != nil {
		conn.Close()
	}
	if n < 16 {
		if Dbglvl > 1 {
			fmt.Printf("received invalid length %d bytes from %s\n", n, raddr)
		}
		return fmt.Errorf("received invalid length %d bytes from %s", n, raddr)
	}
	resp := RespHeader{
		Id:	  binary.BigEndian.Uint32(buf[0:4]),
		Seq:   binary.BigEndian.Uint64(buf[4:12]),
		NameLen: binary.BigEndian.Uint32(buf[12:16]),
	}

	if resp.Id != ID {
		fmt.Printf("received invalid id: %d, expected %d\n", resp.Id, ID)
		return fmt.Errorf("received invalid id: %d, expected %d", resp.Id, ID)
	}
	if Dbglvl > 1 {
		fmt.Printf("Resp header is %v\n", resp)
	}
	if n < int(16 + resp.NameLen) {
		if Dbglvl > 1 {
			fmt.Printf("received invalid length %d bytes from %s\n", n, raddr)
		}
		return fmt.Errorf("received invalid length %d bytes from %s", n, raddr)
	}
	name := string(buf[16:16+resp.NameLen])

	//sequence is index
	rtt := time.Since(serverStatSlice[sindex].ReqStats[resp.Seq].TimeStamp)
	respStat := RespStat {
		Seq: resp.Seq,
		TimeStamp: time.Now(),
		Rtt: rtt,
	}
	if resp.Seq >= uint64(len(serverStatSlice[sindex].ReqStats)) {
		fmt.Printf("receive seq out of range: seq=%d, len=%d, server index=%d, remote=%v\n",
			resp.Seq, len(serverStatSlice[sindex].ReqStats), sindex, conn.RemoteAddr())
		err = &SeqError{ Msg: "receive seq out of range"}
		return
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
		//record at most five lost packet sequences for analysition
		var lostSeqs []uint64
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
				if len(lostSeqs) < 5 {
					lostSeqs = append(lostSeqs, reqStat.Seq)
				}
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
			if Tcp && !MutCT {
				fmt.Printf("send/recv packets number may not match if tcp mutable ct(-m) is not set!\n")
			}
			fmt.Printf("estimate network failure time: %s\n", time.Duration(s.LostNum) * time.Duration(Interval) * time.Millisecond)
			fmt.Printf("print at most 5 no-response packet sequences and their send timestamps:\n")
			for _, seq := range lostSeqs {
				fmt.Printf("no-response packet seq=%d timestamp=%v\n", seq, s.ReqStats[seq].TimeStamp)
			}
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
	if !MutCT {
		if !Tcp {
			raddr := &net.UDPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport}
			var laddr *net.UDPAddr
			if CPort != 0 {
			    laddr = &net.UDPAddr{IP: caddr, Port: CPort}
			} else {
			    laddr = &net.UDPAddr{IP: caddr, Port: CPort}
			}
			c, err = Dial("udp", laddr.String(), raddr.String(), time.Duration(5000), Ssl)
			if err != nil {
				fmt.Printf("dial error: %s\n", err)
				return
			}
		} else {
			raddr := &net.TCPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport}
			var laddr *net.TCPAddr
			if CPort != 0 {
			    laddr = &net.TCPAddr{IP: caddr, Port: CPort}
			} else {
			    laddr = &net.TCPAddr{IP: caddr, Port: CPort}
			}
			c, err = Dial("tcp", laddr.String(), raddr.String(), time.Duration(5000), Ssl)
			if err != nil {
				fmt.Printf("dial error: %s\n", err)
				return
			}
	    	if Ssl {
		    	tconn := c.(CustomTLSConn).GetOriginalConn().(*net.TCPConn)
				SetTcpConnOptions(tconn)
			} else {
				SetTcpConnOptions(c.(*net.TCPConn))
			}
		}
	}
	var RealLen int = PayloadLen
	var payload = make([]byte, RealLen-12)
	if !StrictCheck {
		rand.Read(payload[:])
	}
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

		//only used when it's MutCT and Tcp, in this case we calculate with RTT = payload_receive_time - syn_send_time
		//otherwise,  RTT = payload_receive_time - payload_send_time
		var connectTime time.Time
		if MutCT {
			// c, err = net.DialUDP("udp", &net.UDPAddr{IP: caddr },
				//   &net.UDPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport})	
	   		if !Tcp {
				raddr := &net.UDPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport}

			    var laddr *net.UDPAddr
			    if CPort != 0 {
			        laddr = &net.UDPAddr{IP: caddr, Port: CPort}
			    } else {
			        laddr = &net.UDPAddr{IP: caddr, Port: CPort}
			    }
				c, err = Dial("udp", laddr.String(), raddr.String(), time.Duration(Interval), Ssl)
				if err != nil {
					fmt.Printf("seq = %d, dial error: %s\n", seq, err)
					//dial fail, save a req stat because we tried, fake send
					SendOne(sindex, c, seq, true, time.Time{}, payload)
					seq++
					delay()
					continue
				}
			} else {
				raddr := &net.TCPAddr{IP: serverStatSlice[sindex].Saddr, Port: serverStatSlice[sindex].Sport}
                var laddr *net.TCPAddr
			    if CPort != 0 {
			        laddr = &net.TCPAddr{IP: caddr, Port: CPort}
			    } else {
			        laddr = &net.TCPAddr{IP: caddr, Port: CPort}
			    }
				//for tcp short connection, timeout should not be too short, multiple it with 10
				connectTime = time.Now()
				if Interval < 10 {
					c, err = Dial("tcp", laddr.String(), raddr.String(), time.Duration(Interval * 10), Ssl)
				} else {
					c, err = Dial("tcp", laddr.String(), raddr.String(), time.Duration(Interval), Ssl)
				}
				if err != nil {
					fmt.Printf("seq = %d, dial error: %s\n", seq, err)
					//dial fail, save a req stat because we tried, fake send
					SendOne(sindex, c, seq, true, connectTime, payload)
					seq++
					delay()
					continue
				}
	    		if Ssl {
		    		tconn := c.(CustomTLSConn).GetOriginalConn().(*net.TCPConn)
					SetTcpConnOptions(tconn)
				} else {
					SetTcpConnOptions(c.(*net.TCPConn))
				}
			}
			defer c.Close()
		}
		//send_and_recv()
		err = SendOne(sindex, c, seq, false, connectTime, payload)
		if Tcp {
			if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
				if MutCT {
					seq++
					delay()
					continue
				} else {
					fmt.Printf("seq = %d, the only tcp connection we are using is disconnected\n", seq)
					break
				}
			}
		}
		//why write fail or seq out of range here?
		if errors.Is(err, &SendSeqError) {
			fmt.Printf("seq = %d, send seq error: %s\n", seq, err)
			delay()
			continue
		}
		wg1.Add(1)
		go RecvOne(&wg1, sindex, c, seq)
		seq++
		delay()
		if (MaxPayloadLen != 0) {
			RealLen++
			if RealLen > MaxPayloadLen {
				RealLen = PayloadLen
			}
			payload = make([]byte, RealLen-12)
			if !StrictCheck {
				rand.Read(payload[:])
			}
		}
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
	ID = 0x55aa0000 | (ID & 0x00001111)
	if MutCT {
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
