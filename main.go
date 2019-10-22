package main

import (
	"flag"

	"github.com/abligh/gonbdserver/nbd"

	"github.com/javgh/sia-nbdserver/nbdadapter"
	"github.com/javgh/sia-nbdserver/siaadapter"
)

func main() {
	flag.Parse()

	getSiaReaderWriter := func(size uint64) (nbdadapter.SiaReaderWriter, error) {
		return siaadapter.New(size)
	}
	siaBackendFactory := nbdadapter.NewSiaBackendFactory(getSiaReaderWriter)
	nbd.RegisterBackend("sia", siaBackendFactory)
	nbd.Run(nil)
}
