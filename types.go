package main

// all responses stats
type Stat struct {
	ReqNum  uint64
	RespNum uint64
	LostNum uint64
	AvgRtt  uint64
	MaxRtt  uint64
}

// One Responce stat on the client side
type RespStat struct {
	Seq uint64
	Rtt uint64
}

// responses stats per server
type StatPerServer struct {
	Name    string
	RespNum uint64
	AvgRtt  uint64
	MaxRtt  uint64
	Resps   []RespStat
}

// request packet header
type Request struct {
	Id  uint32
	Seq uint64
	//rest of payload
}

// response packet header
type Response struct {
	Id      uint32
	Seq     uint64
	NameLen uint32
	Name    string //at most 48 bytes
	//rest of payload
}

const MaxPktLen = 9500
const MinPktLen = 64
