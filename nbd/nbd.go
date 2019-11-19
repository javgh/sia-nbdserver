package nbd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
)

type (
	nbdNewStyleHeader struct {
		NbdMagic          uint64
		NbdOptionMagic    uint64
		NbdHandshakeFlags uint16
	}

	nbdClientFlags uint32

	nbdClientOption struct {
		NbdOptionMagic  uint64
		NbdOptionID     uint32
		NbdOptionLength uint32
	}

	nbdOptionReply struct {
		NbdOptionReplyMagic  uint64
		NbdOptionID          uint32
		NbdOptionReplyType   uint32
		NbdOptionReplyLength uint32
	}

	nbdRepInfoPayload struct {
		NbdRepInfoType       uint16
		NbdExportSize        uint64
		NbdTransmissionFlags uint16
	}

	nbdRequest struct {
		NbdRequestMagic uint32
		NbdCommandFlags uint16
		NbdCommandType  uint16
		NbdHandle       uint64
		NbdOffset       uint64
		NbdLength       uint32
	}

	nbdSimpleReply struct {
		NbdSimpleReplyMagic uint32
		NbdError            uint32
		NbdHandle           uint64
	}
)

const (
	nbdMagic            = 0x4e42444d41474943
	nbdOptionMagic      = 0x49484156454F5054
	nbdOptionReplyMagic = 0x3e889045565a9
	nbdRequestMagic     = 0x25609513
	nbdSimpleReplyMagic = 0x67446698

	nbdFlagFixedNewstyle = 1 << 0

	nbdFlagCFixedNewstyle = 1 << 0

	nbdOptAbort = 2
	nbdOptList  = 3
	nbdOptGo    = 7

	nbdRepAck      = 1
	nbdRepServer   = 2
	nbdRepInfo     = 3
	nbdRepErrUnsup = 1<<31 + 1

	nbdInfoExport = 0

	nbdFlagHasFlags = 1 << 0

	nbdCmdRead  = 0
	nbdCmdWrite = 1
	nbdCmdDisc  = 2

	maxOptionLength = 65536

	exportName = "sia"
	exportSize = 1099511627776
)

func handle(conn net.Conn) error {
	newStyleHeader := nbdNewStyleHeader{
		NbdMagic:          nbdMagic,
		NbdOptionMagic:    nbdOptionMagic,
		NbdHandshakeFlags: nbdFlagFixedNewstyle,
	}

	err := binary.Write(conn, binary.BigEndian, newStyleHeader)
	if err != nil {
		return err
	}

	var clientFlags nbdClientFlags
	err = binary.Read(conn, binary.BigEndian, &clientFlags)
	if err != nil {
		return err
	}

	// We will be picky and require the client to have
	// set NBD_FLAG_C_FIXED_NEWSTYLE even though it only SHOULD
	// do so according to the protocol specification.
	if clientFlags != nbdFlagCFixedNewstyle {
		err = conn.Close()
		if err != nil {
			return err
		}

		return errors.New("unexpected client flags")
	}

	handshakeOngoing := true
	for handshakeOngoing {
		var clientOption nbdClientOption
		err = binary.Read(conn, binary.BigEndian, &clientOption)
		if err != nil {
			return err
		}

		if clientOption.NbdOptionMagic != nbdOptionMagic {
			return errors.New("did not receive option magic")
		}

		if clientOption.NbdOptionLength > maxOptionLength {
			return errors.New("option is too long")
		}

		optionData := make([]byte, clientOption.NbdOptionLength)
		if clientOption.NbdOptionLength > 0 {
			_, err = io.ReadFull(conn, optionData)
			if err != nil {
				return err
			}
		}

		switch clientOption.NbdOptionID {
		case nbdOptList:
			optionReply := nbdOptionReply{
				NbdOptionReplyMagic:  nbdOptionReplyMagic,
				NbdOptionID:          clientOption.NbdOptionID,
				NbdOptionReplyType:   nbdRepServer,
				NbdOptionReplyLength: uint32(4 /* length of export name as uint32 */ + len(exportName)),
			}
			err = binary.Write(conn, binary.BigEndian, optionReply)
			if err != nil {
				return err
			}

			err = binary.Write(conn, binary.BigEndian, uint32(len(exportName)))
			if err != nil {
				return err
			}

			err = binary.Write(conn, binary.BigEndian, []byte(exportName))
			if err != nil {
				return err
			}

			optionReply = nbdOptionReply{
				NbdOptionReplyMagic:  nbdOptionReplyMagic,
				NbdOptionID:          clientOption.NbdOptionID,
				NbdOptionReplyType:   nbdRepAck,
				NbdOptionReplyLength: 0,
			}
			err = binary.Write(conn, binary.BigEndian, optionReply)
			if err != nil {
				return err
			}
		case nbdOptAbort:
			optionReply := nbdOptionReply{
				NbdOptionReplyMagic:  nbdOptionReplyMagic,
				NbdOptionID:          clientOption.NbdOptionID,
				NbdOptionReplyType:   nbdRepAck,
				NbdOptionReplyLength: 0,
			}
			err = binary.Write(conn, binary.BigEndian, optionReply)
			if err != nil {
				return err
			}
			return nil
		case nbdOptGo:
			// We won't process the option data that the client
			// sent along. The export name doesn't matter and
			// dealing with any information requests the client may have
			// is not implemented.

			// send NBD_INFO_EXPORT
			optionReply := nbdOptionReply{
				NbdOptionReplyMagic:  nbdOptionReplyMagic,
				NbdOptionID:          clientOption.NbdOptionID,
				NbdOptionReplyType:   nbdRepInfo,
				NbdOptionReplyLength: 12, // size of nbdRepInfoPayload struct
			}
			err = binary.Write(conn, binary.BigEndian, optionReply)
			if err != nil {
				return err
			}

			infoPayload := nbdRepInfoPayload{
				NbdRepInfoType:       nbdInfoExport,
				NbdExportSize:        exportSize,
				NbdTransmissionFlags: nbdFlagHasFlags,
			}
			err = binary.Write(conn, binary.BigEndian, infoPayload)
			if err != nil {
				return err
			}

			// send NBD_REP_ACK
			optionReply = nbdOptionReply{
				NbdOptionReplyMagic:  nbdOptionReplyMagic,
				NbdOptionID:          clientOption.NbdOptionID,
				NbdOptionReplyType:   nbdRepAck,
				NbdOptionReplyLength: 0,
			}
			err = binary.Write(conn, binary.BigEndian, optionReply)
			if err != nil {
				return err
			}

			// entering transmission phase now
			handshakeOngoing = false
		default:
			// reply with 'not supported' for everything else
			optionReply := nbdOptionReply{
				NbdOptionReplyMagic:  nbdOptionReplyMagic,
				NbdOptionID:          clientOption.NbdOptionID,
				NbdOptionReplyType:   nbdRepErrUnsup,
				NbdOptionReplyLength: 0,
			}
			err = binary.Write(conn, binary.BigEndian, optionReply)
			if err != nil {
				return err
			}
		}
	}

	transmissionOngoing := true
	for transmissionOngoing {
		var request nbdRequest
		err = binary.Read(conn, binary.BigEndian, &request)
		if err != nil {
			return err
		}

		if request.NbdRequestMagic != nbdRequestMagic {
			return errors.New("did not receive request magic")
		}

		b := make([]byte, request.NbdLength)
		switch request.NbdCommandType {
		case nbdCmdRead:
			fmt.Println("read", request.NbdLength)
			reply := nbdSimpleReply{
				NbdSimpleReplyMagic: nbdSimpleReplyMagic,
				NbdError:            0,
				NbdHandle:           request.NbdHandle,
			}
			err = binary.Write(conn, binary.BigEndian, reply)
			if err != nil {
				return err
			}

			err = binary.Write(conn, binary.BigEndian, b)
			if err != nil {
				return err
			}
		case nbdCmdWrite:
			fmt.Println("write", request.NbdLength)
			_, err = io.ReadFull(conn, b)
			if err != nil {
				return err
			}

			reply := nbdSimpleReply{
				NbdSimpleReplyMagic: nbdSimpleReplyMagic,
				NbdError:            0,
				NbdHandle:           request.NbdHandle,
			}
			err = binary.Write(conn, binary.BigEndian, reply)
			if err != nil {
				return err
			}
		case nbdCmdDisc:
			transmissionOngoing = false
		}
	}

	return nil
}

func Playground() {
	ln, err := net.Listen("unix", "/tmp/playground")
	if err != nil {
		log.Fatal(err)
	}

	conn, err := ln.Accept()
	if err != nil {
		log.Fatal(err)
	}

	err = handle(conn)
	if err != nil {
		log.Fatal(err)
	}

	err = conn.Close()
	if err != nil {
		log.Fatal(err)
	}

	err = ln.Close()
	if err != nil {
		log.Fatal(err)
	}
}
