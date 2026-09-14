package client

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	pb "github.com/OcupointInc/rf-control/controlpb"
	"google.golang.org/protobuf/proto"
)

func TestTCPTransportReadsCompleteResponse(t *testing.T) {
	for _, size := range []int{0, 32, 8192} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Parallel()
			response := &pb.Packet{MessageId: &pb.Packet_ErrorResponse{ErrorResponse: &pb.ErrorResponse{
				Detail: strings.Repeat("x", size),
			}}}
			if size == 0 {
				response = &pb.Packet{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}}
			}
			wire, err := proto.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			defer close(done)
			serverErr := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					serverErr <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				if _, err := readTCPResponse(conn); err != nil {
					serverErr <- err
					return
				}
				// Split both header varints and the body across TCP writes.
				for len(wire) > 0 {
					chunk := 1
					if len(wire) > 20 && len(wire) < size {
						chunk = 17
					}
					if _, err := conn.Write(wire[:chunk]); err != nil {
						serverErr <- err
						return
					}
					wire = wire[chunk:]
					if chunk == 1 {
						time.Sleep(time.Millisecond)
					}
				}
				serverErr <- nil
				// A successful read must finish without waiting for server EOF.
				<-done
			}()
			tx := &TCPTransport{addr: listener.Addr().String()}
			got, err := tx.Send(&pb.Packet{MessageId: &pb.Packet_GetStatusRequest{GetStatusRequest: &pb.GetStatusRequest{}}})
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(got, response) {
				t.Fatalf("response did not match (%d-byte detail)", size)
			}
			if err := <-serverErr; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReadTCPResponseRejectsInvalidOrTruncatedPacket(t *testing.T) {
	oversized := binary.AppendUvarint(nil, 10)
	oversized = binary.AppendUvarint(oversized, tcpMaxResponseSize)
	for _, test := range []struct {
		name string
		wire []byte
	}{
		{"missing tag", nil},
		{"truncated tag", []byte{0x80}},
		{"tag zero", []byte{0, 0}},
		{"wrong wire type", []byte{8, 0}},
		{"truncated length", []byte{10, 0x80}},
		{"length overflow", append([]byte{10}, bytes.Repeat([]byte{0xff}, 10)...)},
		{"oversized", oversized},
		{"truncated body", []byte{10, 3, 1}},
		{"unknown message", []byte{0xfa, 0x7f, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := readTCPResponse(bytes.NewReader(test.wire)); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}

func TestReadTCPResponseStopsAfterOnePacket(t *testing.T) {
	response := &pb.Packet{MessageId: &pb.Packet_GetStatusResponse{GetStatusResponse: &pb.GetStatusResponse{BoardType: "barracuda"}}}
	wire, err := proto.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	// io.MultiReader returns an error if the parser attempts another read
	// after the complete response. No EOF delimiter is required.
	got, err := readTCPResponse(io.MultiReader(bytes.NewReader(wire), tcpErrorReader{}))
	if err != nil || !proto.Equal(got, response) {
		t.Fatalf("got %v, error %v", got, err)
	}
}

type tcpErrorReader struct{}

func (tcpErrorReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("unexpected read beyond response")
}
