package client

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	pb "github.com/OcupointInc/rf-control/controlpb"
	"google.golang.org/protobuf/proto"
)

const (
	FirmwareUpdatePort = 5002
	firmwareSlotSize   = 0x0FB000
	imageInfoMagic0    = 0x4F435550
	imageInfoMagic1    = 0x46574930
	imageBoardLength   = 24
	imageVersionLength = 16
	imageBuildIDLength = 24
	imageInfoSize      = 72

	firmwareDialTimeout     = 5 * time.Second
	firmwareBeginTimeout    = 20 * time.Second
	firmwareChunkTimeout    = 5 * time.Second
	firmwareCommitTimeout   = 20 * time.Second
	firmwareTransferTimeout = 5 * time.Minute
	firmwareMaxAckRetries   = 3
	firmwareMaxStalls       = 100
)

// FirmwareImage is a validated Barracuda OTA .bin with its embedded identity
// and whole-image IEEE CRC-32. Data is the exact byte sequence sent to firmware.
type FirmwareImage struct {
	Data       []byte
	Board      string
	Version    string
	BuildID    string
	InfoOffset int
	CRC32      uint32
}

func (i *FirmwareImage) Size() uint32 {
	if i == nil {
		return 0
	}
	return uint32(len(i.Data))
}

// LoadFirmwareImage reads and validates an OTA application .bin. Factory UF2
// images are not OTA payloads and intentionally fail this parser.
func LoadFirmwareImage(path string) (*FirmwareImage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseFirmwareImage(data)
}

func parseFirmwareImage(data []byte) (*FirmwareImage, error) {
	if len(data) == 0 {
		return nil, errors.New("firmware image is empty")
	}
	if len(data) > firmwareSlotSize {
		return nil, fmt.Errorf("firmware image is %d bytes; update slot holds %d", len(data), firmwareSlotSize)
	}
	for offset := 0; offset+8 <= len(data); offset += 4 {
		if binary.LittleEndian.Uint32(data[offset:]) != imageInfoMagic0 ||
			binary.LittleEndian.Uint32(data[offset+4:]) != imageInfoMagic1 {
			continue
		}
		if offset+imageInfoSize > len(data) {
			return nil, fmt.Errorf("firmware identity at offset %d is truncated", offset)
		}
		block := data[offset : offset+imageInfoSize]
		board := cString(block[8 : 8+imageBoardLength])
		if board == "" {
			return nil, errors.New("firmware image has an empty board identity")
		}
		return &FirmwareImage{
			Data:       data,
			Board:      board,
			Version:    cString(block[8+imageBoardLength : 8+imageBoardLength+imageVersionLength]),
			BuildID:    cString(block[8+imageBoardLength+imageVersionLength : imageInfoSize]),
			InfoOffset: offset,
			CRC32:      crc32.ChecksumIEEE(data),
		}, nil
	}
	return nil, errors.New("firmware image identity not found (expected OCUP/FWI0 block)")
}

func cString(value []byte) string {
	if end := bytes.IndexByte(value, 0); end >= 0 {
		value = value[:end]
	}
	return string(value)
}

type firmwareRoundTripper interface {
	roundTrip(*pb.Packet, time.Duration) (*pb.Packet, error)
	Close() error
}

type firmwareTCPTransport struct {
	connection net.Conn
	reader     *bufio.Reader
}

func newFirmwareTCPTransport(host string, port int) (*firmwareTCPTransport, error) {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	connection, err := net.DialTimeout("tcp", address, firmwareDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", address, err)
	}
	return &firmwareTCPTransport{connection: connection, reader: bufio.NewReader(connection)}, nil
}

func (t *firmwareTCPTransport) Close() error { return t.connection.Close() }

func (t *firmwareTCPTransport) roundTrip(request *pb.Packet, timeout time.Duration) (*pb.Packet, error) {
	payload, err := proto.Marshal(request)
	if err != nil {
		return nil, err
	}
	if len(payload) > 0xffff {
		return nil, fmt.Errorf("firmware update payload too large: %d", len(payload))
	}
	if err := t.connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	frame := make([]byte, 4+len(payload))
	frame[0], frame[1] = usbFrameMagic0, usbFrameMagic1
	binary.LittleEndian.PutUint16(frame[2:4], uint16(len(payload)))
	copy(frame[4:], payload)
	if _, err := t.connection.Write(frame); err != nil {
		return nil, err
	}
	responsePayload, err := readFirmwareFrame(t.reader)
	if err != nil {
		return nil, err
	}
	response := &pb.Packet{}
	if err := proto.Unmarshal(responsePayload, response); err != nil {
		return nil, err
	}
	return response, nil
}

func readFirmwareFrame(reader *bufio.Reader) ([]byte, error) {
	for {
		first, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if first != usbFrameMagic0 {
			continue
		}
		second, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if second == usbFrameMagic1 {
			break
		}
		if second == usbFrameMagic0 {
			if err := reader.UnreadByte(); err != nil {
				return nil, err
			}
		}
	}
	var lengthBytes [2]byte
	if _, err := io.ReadFull(reader, lengthBytes[:]); err != nil {
		return nil, err
	}
	payload := make([]byte, binary.LittleEndian.Uint16(lengthBytes[:]))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

type firmwareUSBTransport struct{ usb *USBTransport }

func (t *firmwareUSBTransport) Close() error { return nil }

func (t *firmwareUSBTransport) roundTrip(request *pb.Packet, timeout time.Duration) (*pb.Packet, error) {
	previous := t.usb.timeout
	t.usb.timeout = timeout
	defer func() { t.usb.timeout = previous }()
	return t.usb.Send(request)
}

// UpdateFirmwareTCP streams image to the dedicated framed Ethernet update
// service. progress receives acknowledged and total byte counts.
func UpdateFirmwareTCP(host string, port int, image *FirmwareImage, progress func(done, total uint32)) error {
	transport, err := newFirmwareTCPTransport(host, port)
	if err != nil {
		return err
	}
	defer transport.Close()
	return runFirmwareUpdate(transport, image, progress)
}

// UpdateFirmwareUSB streams image over an already-open Barracuda USB control
// port. The caller still owns usb and should close it when finished.
func UpdateFirmwareUSB(usb *USBTransport, image *FirmwareImage, progress func(done, total uint32)) error {
	if usb == nil {
		return errors.New("USB transport is nil")
	}
	return runFirmwareUpdate(&firmwareUSBTransport{usb: usb}, image, progress)
}

func runFirmwareUpdate(transport firmwareRoundTripper, image *FirmwareImage, progress func(done, total uint32)) (resultErr error) {
	if image == nil || len(image.Data) == 0 {
		return errors.New("firmware image is nil or empty")
	}
	if image.Board == "" || image.Size() > firmwareSlotSize {
		return errors.New("firmware image identity or size is invalid")
	}
	if currentCRC := crc32.ChecksumIEEE(image.Data); currentCRC != image.CRC32 {
		return fmt.Errorf("firmware image changed after loading (CRC is 0x%08X, expected 0x%08X)", currentCRC, image.CRC32)
	}
	abort := func() {
		_, _ = transport.roundTrip(&pb.Packet{MessageId: &pb.Packet_FwUpdateAbortRequest{
			FwUpdateAbortRequest: &pb.FwUpdateAbortRequest{},
		}}, 2*time.Second)
	}
	defer func() {
		if resultErr != nil {
			abort()
		}
	}()

	response, err := transport.roundTrip(&pb.Packet{MessageId: &pb.Packet_FwUpdateBeginRequest{
		FwUpdateBeginRequest: &pb.FwUpdateBeginRequest{
			Size: image.Size(), Crc32: image.CRC32, Board: image.Board, Version: image.Version,
		},
	}}, firmwareBeginTimeout)
	if err != nil {
		return fmt.Errorf("begin update: %w", err)
	}
	if err := firmwareDeviceError(response); err != nil {
		return err
	}
	begin := response.GetFwUpdateBeginResponse()
	if begin == nil || begin.MaxChunk == 0 {
		return fmt.Errorf("begin update: invalid response %T", response.MessageId)
	}

	total := image.Size()
	if progress != nil {
		progress(0, total)
	}
	var offset, highWater uint32
	stalls, retries := 0, 0
	started := time.Now()
	for offset < total {
		if time.Since(started) > firmwareTransferTimeout {
			return fmt.Errorf("firmware transfer timed out at %d/%d bytes", offset, total)
		}
		end := offset + begin.MaxChunk
		if end > total {
			end = total
		}
		response, err = transport.roundTrip(&pb.Packet{MessageId: &pb.Packet_FwUpdateDataRequest{
			FwUpdateDataRequest: &pb.FwUpdateDataRequest{Offset: offset, Data: image.Data[offset:end]},
		}}, firmwareChunkTimeout)
		if err != nil {
			if isFirmwareTimeout(err) && retries < firmwareMaxAckRetries {
				retries++
				continue
			}
			return fmt.Errorf("firmware data at %d: %w", offset, err)
		}
		retries = 0
		if err := firmwareDeviceError(response); err != nil {
			return err
		}
		dataResponse := response.GetFwUpdateDataResponse()
		if dataResponse == nil || dataResponse.NextOffset > total {
			return fmt.Errorf("firmware data at %d: invalid response", offset)
		}
		next := dataResponse.NextOffset
		if next > highWater {
			highWater, stalls = next, 0
		} else {
			stalls++
			if stalls > firmwareMaxStalls {
				return fmt.Errorf("firmware transfer stalled at %d", next)
			}
		}
		offset = next
		if progress != nil {
			progress(offset, total)
		}
	}

	response, err = transport.roundTrip(&pb.Packet{MessageId: &pb.Packet_FwUpdateCommitRequest{
		FwUpdateCommitRequest: &pb.FwUpdateCommitRequest{},
	}}, firmwareCommitTimeout)
	if err != nil {
		return fmt.Errorf("commit update: %w", err)
	}
	if err := firmwareDeviceError(response); err != nil {
		return err
	}
	commit := response.GetFwUpdateCommitResponse()
	if commit == nil || !commit.CrcOk {
		return errors.New("device rejected firmware CRC; image was not applied")
	}
	return nil
}

func firmwareDeviceError(response *pb.Packet) error {
	if deviceError := response.GetErrorResponse(); deviceError != nil {
		if deviceError.Detail != "" {
			return fmt.Errorf("device error: %s (%s)", deviceError.Detail, deviceError.Code)
		}
		return fmt.Errorf("device error: %s", deviceError.Code)
	}
	return nil
}

func isFirmwareTimeout(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}
