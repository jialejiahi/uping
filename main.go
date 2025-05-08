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
	MaxPayloadLen int // default 0, 最大请求负载长度
	Interval   int //default 100, 单位ms
	Count      uint64 //发送报文的数量
	Timeout    int //认为报文无应答的超时时间
	Tcp 	   bool //use tcp, default false
	Ssl bool //use ssl, default false
	Check	   bool //check request format on serverside, default false, Server Only
	StrictCheck bool //client send request with fixed content, on server side, it means check the content
	NoDelay bool      //disable nodelay on tcp sockets
	EnableQuickAck bool      //enable quick ack on tcp sockets
)

var Interrupted bool
var buildtime string

func init() {
	flag.BoolVar(&help, "h", false, "Show the help message")
	flag.IntVar(&Dbglvl, "d", 1, "Debug level 0-3")
	flag.StringVar(&SAddr, "B", "0.0.0.0", "Server Binding Address, Must be set if run as Client")
	flag.StringVar(&SPortList, "P", "23456,23457", "Server Data Port List")
	flag.BoolVar(&Tcp, "T", false, "Use TCP Protocol, default false")
	flag.BoolVar(&Ssl, "E", false, "Use SSL/TLS Protocol, default false")
	flag.BoolVar(&NoDelay, "N", true, "Disable NoDelay on tcp sockets, default true")
	flag.BoolVar(&EnableQuickAck, "Q", false, "Enable Quick Ack on tcp sockets, default false")

	flag.BoolVar(&IsServer, "s", false, "Run as server, Server Only")
	flag.StringVar(&Name, "n", "", "Server Host Name, Get Host Name if it's not given, Server Only")
	flag.BoolVar(&Check, "C", false, "Check request format on serverside, default false, Server Only")
	flag.BoolVar(&StrictCheck, "S", false, "Strict check payload on serverside, send fix content on client side, default false")

	flag.StringVar(&CAddr, "b", "0.0.0.0", "Client Binding Address, Client Only")
	flag.IntVar(&CPort, "p", 0, "Client Binding Port, Client Only")
	flag.BoolVar(&MutCT, "m", false, "Mutable Connection(close connection every packet), Client Only")
	flag.IntVar(&PayloadLen, "l", 64, "Payload Length, Client Only")
	flag.IntVar(&MaxPayloadLen, "L", 64, "Max Payload Length, Client Only")
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
  -E bool
        Encrypt tcp Using ssl (default false)
        使用SSL协议加密tcp会话, 默认false
  -N bool
        Set NoDelay on tcp sockets (default true)
        开启tcp协议的NoDelay选项,关闭Nagle算法; disable将合并小包发送,对丢包探测有影响，不建议Disable NoDelay
  -Q bool
        Enable Quick Ack on tcp sockets (default false)
        开启tcp协议的QuickAck选项,开启后将会立即回复Ack小包,不等待发包时携带
  -s bool
        Run as server (default false)
        作为server运行,不指定则作为client运行
  -n name
        Server Name, Get Hostname if it's not given
        服务端指定,用于客户端区分多个server,不指定时获取系统Hostname
  -C bool
        Check Request format on serverside (default false)
        服务端收包时是否检查请求格式
  -S bool
        Send fix content from client and Check payload on serverside (default false)
        严格检查: 客户端设置时发固定payload内容, 服务端设置时检查payload内容是否正确
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
  -L int
        Requests Max Length, Client Only (default 0)
        设置为一个大于 -l 的值时，将会在[l,L]范围内递增负载长度
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
func write_proc_file(file_name string, value string) {
	_, err := os.Stat(file_name)
	if err != nil {
		if os.IsNotExist(err) {
			//fmt.Println("文件不存在")
			return
		} else {
			//fmt.Println("发生错误:", err)
			return
		}
	}
	f, err := os.OpenFile(file_name, os.O_RDWR, 0)
	if err != nil {
		//fmt.Printf("open %s error: %s", file_name, err.Error())
		return	
	}
	defer f.Close()
	f.WriteString(value)
}

func set_server_syn_backlog_cookie() {
	write_proc_file("/proc/sys/net/core/somaxconn", "1024")

	write_proc_file("/proc/sys/net/ipv4/tcp_syncookies", "0")
}

func set_socket_buf_size() {
	write_proc_file("/proc/sys/net/core/rmem_max", "2097152")

	write_proc_file("/proc/sys/net/core/rmem_default", "2097152")
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
		if MaxPayloadLen <= PayloadLen {
			MaxPayloadLen = 0
		}
		if MaxPayloadLen > MaxPktLen {
			MaxPayloadLen = MaxPktLen
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
