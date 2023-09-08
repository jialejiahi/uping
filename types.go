package main

import (
	"net"
	"sync"
	"time"
)

// request packet header
type ReqHeader struct {
	Id        uint32
	Seq 	  uint64
}

// response packet header
type RespHeader struct {
	Id      uint32
	Seq     uint64
	NameLen uint32
	//Name    string
}
// request packet header stat
type ReqStat struct {
	//Id
	Seq 	  uint64
	TimeStamp time.Time //Not in requst payload
	RespStatPtr *RespStat
	//rest of payload
}

// One Responce stat on the client side
type RespStat struct {
	Seq uint64
	Rtt time.Duration
	TimeStamp time.Time
}

type StatPerName struct {
	Name    string
	RespStats   []RespStat
	RespLock *sync.RWMutex
}

// stats per server
type StatPerServer struct {
	//Addr 	string
	Saddr	net.IP
	Sport 	int
	ReqNum  uint64
	RespNum uint64
	LostNum uint64
	TotalRtt  time.Duration
	AvgRtt  time.Duration
	MaxRtt  time.Duration
	ReqStats []ReqStat
	ReqLock *sync.RWMutex
	StatPerNames []StatPerName
}

const MaxPktLen = 9500
const MinPktLen = 64
