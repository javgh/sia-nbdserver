package nbd

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"time"
)

type (
	Backend interface {
		Available() bool
		ReadAt(buf []byte, offset int64) (int, error)
		WriteAt(buf []byte, offset int64) (int, error)
	}

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

	maxOptionLength  = 65536
	maxRequestLength = 268435456

	exportName        = "sia"
	interruptInterval = 2 * time.Second
)

func handle(conn net.Conn, exportSize uint64, backend Backend) error {
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

	buf := make([]byte, 0)
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

		if request.NbdLength > maxRequestLength {
			return errors.New("request is too large")
		}

		if int(request.NbdLength) > cap(buf) {
			// increase buffer capacity as needed
			buf = make([]byte, request.NbdLength)
		}
		buf = buf[0:request.NbdLength]

		switch request.NbdCommandType {
		case nbdCmdRead:
			_, err := backend.ReadAt(buf, int64(request.NbdOffset))
			if err != nil {
				// Taking some liberty with error handling
				// and just disconnecting here.
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

			err = binary.Write(conn, binary.BigEndian, buf)
			if err != nil {
				return err
			}
		case nbdCmdWrite:
			_, err = io.ReadFull(conn, buf)
			if err != nil {
				return err
			}

			_, err := backend.WriteAt(buf, int64(request.NbdOffset))
			if err != nil {
				// Taking some liberty with error handling
				// and just disconnecting here.
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

func Serve(socketPath string, exportSize uint64, backend Backend) error {
	unixAddr, err := net.ResolveUnixAddr("unix", socketPath)
	if err != nil {
		return err
	}

	ln, err := net.ListenUnix("unix", unixAddr)
	if err != nil {
		return err
	}
	log.Printf("Server listens at %s - connect with:\n", socketPath)
	log.Printf("  # modprobe nbd\n")
	log.Printf("  # nbd-client -b 4096 -u %s /dev/nbd0\n", socketPath)

	for backend.Available() {
		// Wake up from Accept() periodically to
		// check if we need to shutdown the server.
		ln.SetDeadline(time.Now().Add(interruptInterval))
		conn, err := ln.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}
			return err
		}
		log.Printf("Client connected")

		err = handle(conn, exportSize, backend)
		if err != nil {
			log.Printf("Client disconnected with error: %s", err)
		} else {
			log.Printf("Client disconnected")
		}

		err = conn.Close()
		if err != nil {
			return err
		}
	}

	err = ln.Close()
	if err != nil {
		return err
	}

	return nil
}
