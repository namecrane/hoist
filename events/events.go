package events

import "github.com/philippseith/signalr"

type Receiver struct {
	signalr.Hub
}

type SelfTest struct {
	TestStr string `json:"testStr"`
}

func (r *Receiver) SelfTestReturn(data SelfTest) {}
