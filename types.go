package main

import (
	"sync"
	"time"
)

// request packet header stat
type ReqStat struct {
	//Id
	Seq 	  uint64
	TimeStamp time.Time //Not in requst payload
	RespStatPtr *RespStat
	//rest of payload
}

// all responses stats
type Stat struct {
	ReqNum  uint64
	RespNum uint64
	LostNum uint64
	AvgRtt  time.Duration
	MaxRtt  time.Duration
	Id  uint32
	ReqStats []ReqStat
	ReqLock *sync.RWMutex
}

// One Responce stat on the client side
type RespStat struct {
	Seq uint64
	Rtt time.Duration
	TimeStamp time.Time
}

// responses stats per server
type StatPerServer struct {
	Addr 	string
	Name    string
	RespNum uint64
	AvgRtt  uint64
	MaxRtt  uint64
	RespStats   []*RespStat
	RespLock *sync.RWMutex
}

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

const MaxPktLen = 9500
const MinPktLen = 64
