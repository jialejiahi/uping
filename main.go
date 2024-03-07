package main

import (
	"flag"
	"fmt"
	"net"

	//_ "net/http/pprof"
	"os"
	"strconv"
	"strings"
)

var (
	SAddr      string
	SPortList  string // default 60000;
	Dbglvl     int    // default 1;
	help       bool
	IsServer   bool   //default false, run as server
	Name       string //server name, default is hostname
	CAddr      string
	CPort      int
	MutCT   bool
	PayloadLen int // default 64; 每个请求的负载长度
	Interval   int //default 100, 单位ms
	Count      uint64 //发送报文的数量
	Timeout    int //认为报文无应答的超时时间
	Tcp 	   bool //use tcp, default false
	Check	   bool //check request format on serverside, default false, Server Only
)

var Interrupted bool
var buildtime string

func init() {
	flag.BoolVar(&help, "h", false, "Show the help message")
	flag.IntVar(&Dbglvl, "d", 1, "Debug level 0-3")
	flag.StringVar(&SAddr, "B", "0.0.0.0", "Server Binding Address, Must be set if run as Client")
	flag.StringVar(&SPortList, "P", "23456,23457", "Server Data Port List")
	flag.BoolVar(&Tcp, "T", false, "Use TCP Protocol, default false")

	flag.BoolVar(&IsServer, "s", false, "Run as server, Server Only")
	flag.StringVar(&Name, "n", "", "Server Host Name, Get Host Name if it's not given, Server Only")
	flag.BoolVar(&Check, "C", false, "Check request format on serverside, default false, Server Only")

	flag.StringVar(&CAddr, "b", "0.0.0.0", "Client Binding Address, Client Only")
	flag.IntVar(&CPort, "p", 0, "Client Binding Port, Client Only")
	flag.BoolVar(&MutCT, "m", false, "Mutable Connection(close connection every packet), Client Only")
	flag.IntVar(&PayloadLen, "l", 64, "Payload Length, Client Only")
	flag.IntVar(&Interval, "i", 100, "New Request Interval in ms, Client Only")
	flag.Uint64Var(&Count, "c", 10, "Requests per data socket, Client Only")
	flag.IntVar(&Timeout, "t", 1000, "Receive Response Timeout in ms, Client Only")
}

func checkError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return
	}
}

func Usage() {
	str := `
  -h Show help message
  -d int
        Debug Output level 0-2 (default 1)
        打印级别 0. 不打印调试信息 1. 基本输出 2. 详细输出
  -B string
        Server Binding Address, must if run as client (default "0.0.0.0")
        服务端绑定的地址,服务端可选,客户端必填
  -P string
        Server Data Port List, format: 23456,23457
        服务端监听的端口列表,客户端指定多个端口时,将循环遍历端口列表
  -T bool
        Use tcp instead of udp for ping (default false)
        使用tcp协议而不是udp, 默认false
  -s bool
        Run as server (default false)
        作为server运行,不指定则作为client运行
  -n name
        Server Name, Get Hostname if it's not given
        服务端指定,用于客户端区分多个server,不指定时获取系统Hostname
  -C bool
        Check Request format on serverside (default false)
        服务端收包时是否检查请求格式
  -b string
        Client Binding Address, Client Only (default "0.0.0.0")
        client端绑定的地址, 不指定则不绑定
  -p int
        Client Binding Port, Client Only (default 0)
        client端绑定的端口,不绑定则使用随机值
  -m bool
        Client Mutable Connection, Client Only (default false)
        使用短连接发包, 每次发包时新建连接，完成后断开, 默认false
  -l int
        Requests Length, Client Only (default 64)
        请求负载的长度, 最小取值64字节, 以容纳自定义负载头部
  -i int
        Request Sending Interval, Client Only (default 100)
        发包间隔,单位毫秒,默认值100
  -c int
        Request Count, Client Only (default 10)
        请求个数, client 发送探测请求的个数
  -t int
        Receive Timeout in ms, Client Only (default 1000)
        请求报文无应答的超时时间,单位毫秒,默认值1000
`
	fmt.Printf("Build Time is: %s\n", buildtime)
	fmt.Print(str)
}

func GetPortList(pliststr string) (plist []uint16, err error) {
	a := strings.Split(pliststr, ",")
	plist = make([]uint16, len(a))
	for i, v := range a {
		port, e := strconv.Atoi(v)
		if e != nil {
			fmt.Printf("PortList format error: %s", err.Error())
			err = e
			plist = []uint16{}
			return
		} else {
			plist[i] = uint16(port)
		}
	}
	return
}

// server 端并发接收设置
// 设大listen backlog
// sysctl -w net.core.somaxconn=1024
// 关闭syn cookie
// sysctl -w net.ipv4.tcp_syncookies=0
func set_server_syn_backlog_cookie() {
	f, err := os.OpenFile("/proc/sys/net/core/somaxconn", os.O_RDWR, 0)
	if err != nil {
		fmt.Printf("open /proc/sys/net/core/somaxconn error: %s", err.Error())
		return	
	}
	defer f.Close()
	//default is 128
	f.WriteString("1024")

	f, err = os.OpenFile("/proc/sys/net/ipv4/tcp_syncookies", os.O_RDWR, 0)
	if err != nil {
		fmt.Printf("open /proc/sys/net/ipv4/tcp_syncookies error: %s", err.Error())
		return	
	}
	defer f.Close()
	//default is 1
	f.WriteString("0")
}

func set_socket_buf_size() {
	f, err := os.OpenFile("/proc/sys/net/core/rmem_max", os.O_RDWR, 0)
	if err != nil {
		fmt.Printf("open /proc/sys/net/core/rmem_max error: %s", err.Error())
		return	
	}
	defer f.Close()
	f.WriteString("2097152")

	f, err = os.OpenFile("/proc/sys/net/core/rmem_default", os.O_RDWR, 0)
	if err != nil {
		fmt.Printf("open /proc/sys/net/core/rmem_default error: %s", err.Error())
		return	
	}
	defer f.Close()
	f.WriteString("2097152")
}

func main() {
	//for gprof debug
	//go func() {
	//	log.Println(http.ListenAndServe("localhost:6060", nil))
	//}()
	if len(os.Args) < 2 {
		Usage()
		return
	}
	flag.Parse()
	if help {
		Usage()
		return
	}
	//Dbglvl 0: 只打印报告,不打印调试信息
	//Dbglvl 1: 打印控制线程的调试信息
	//Dbglvl 2: 打印每个数据线程的调试信息, per server port
	//Dbglvl 3: 打印所有连接和请求的调试信息, per client port
	if Dbglvl > 2 || Dbglvl < 0 {
		Dbglvl = 2
	}
	plist, err := GetPortList(SPortList)
	if err != nil {
		fmt.Printf("PortList format error: %s", err.Error())
		return
	}

	if !IsServer {
		saddr := net.ParseIP(SAddr)
		if saddr == nil || saddr.Equal(net.ParseIP("0.0.0.0")) {
			fmt.Printf("Legal Server IP address must be set with -B when the program run as client!\n")
			return
		}
		caddr := net.ParseIP(CAddr)
		if caddr == nil {
			fmt.Printf("Client IP Address %s illegal!\n", CAddr)
			return
		}

		if PayloadLen > MaxPktLen {
			fmt.Printf("Request len truncate to %d\n", MaxPktLen)
			PayloadLen = MaxPktLen
		}
		if PayloadLen < MinPktLen {
			fmt.Printf("Request at least %d, set to %d\n", MinPktLen, MinPktLen)
			PayloadLen = 64
		}
		set_socket_buf_size()
		client_main(saddr, caddr, plist)

	} else {
		saddr := net.ParseIP(SAddr)
		if saddr == nil {
			fmt.Printf("Server IP Address %s illegal!\n", SAddr)
			Usage()
			return
		}
		set_socket_buf_size()
		set_server_syn_backlog_cookie()
		server_main(saddr, plist)
	}
}
