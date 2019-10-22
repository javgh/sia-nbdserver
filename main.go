package main

import (
	"flag"

	"github.com/abligh/gonbdserver/nbd"

	"github.com/javgh/sia-nbdserver/nbdadapter"
	"github.com/javgh/sia-nbdserver/siaadapter"
)

func main() {
	flag.Parse()
	siaReaderWriter := siaadapter.New()
	nbd.RegisterBackend("sia", nbdadapter.NewSiaBackend(siaReaderWriter))
	nbd.Run(nil)
}
