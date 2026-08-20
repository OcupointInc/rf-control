package client

import (
	"encoding/binary"
	"testing"
	"time"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

func firmwareFixture() []byte {
	data := make([]byte, 160)
	offset := 16
	binary.LittleEndian.PutUint32(data[offset:], imageInfoMagic0)
	binary.LittleEndian.PutUint32(data[offset+4:], imageInfoMagic1)
	copy(data[offset+8:offset+8+imageBoardLength], "barracuda")
	copy(data[offset+8+imageBoardLength:offset+8+imageBoardLength+imageVersionLength], "1.7.2")
	copy(data[offset+8+imageBoardLength+imageVersionLength:offset+imageInfoSize], "test-build")
	return data
}

func TestParseFirmwareImage(t *testing.T) {
	image, err := parseFirmwareImage(firmwareFixture())
	if err != nil {
		t.Fatal(err)
	}
	if image.Board != "barracuda" || image.Version != "1.7.2" || image.BuildID != "test-build" || image.InfoOffset != 16 {
		t.Fatalf("unexpected image metadata: %+v", image)
	}
	if image.Size() != 160 || image.CRC32 == 0 {
		t.Fatalf("unexpected image size/CRC: %d 0x%08x", image.Size(), image.CRC32)
	}
}

func TestParseFirmwareImageRejectsMissingIdentity(t *testing.T) {
	if _, err := parseFirmwareImage(make([]byte, 128)); err == nil {
		t.Fatal("expected missing identity error")
	}
}

type fakeFirmwareTransport struct {
	t       *testing.T
	offset  uint32
	begin   *pb.FwUpdateBeginRequest
	commits int
}

func (f *fakeFirmwareTransport) Close() error { return nil }

func (f *fakeFirmwareTransport) roundTrip(packet *pb.Packet, _ time.Duration) (*pb.Packet, error) {
	switch request := packet.MessageId.(type) {
	case *pb.Packet_FwUpdateBeginRequest:
		f.begin = request.FwUpdateBeginRequest
		return &pb.Packet{MessageId: &pb.Packet_FwUpdateBeginResponse{
			FwUpdateBeginResponse: &pb.FwUpdateBeginResponse{MaxChunk: 32},
		}}, nil
	case *pb.Packet_FwUpdateDataRequest:
		if request.FwUpdateDataRequest.Offset != f.offset {
			f.t.Fatalf("got offset %d, want %d", request.FwUpdateDataRequest.Offset, f.offset)
		}
		f.offset += uint32(len(request.FwUpdateDataRequest.Data))
		return &pb.Packet{MessageId: &pb.Packet_FwUpdateDataResponse{
			FwUpdateDataResponse: &pb.FwUpdateDataResponse{NextOffset: f.offset},
		}}, nil
	case *pb.Packet_FwUpdateCommitRequest:
		f.commits++
		return &pb.Packet{MessageId: &pb.Packet_FwUpdateCommitResponse{
			FwUpdateCommitResponse: &pb.FwUpdateCommitResponse{CrcOk: true},
		}}, nil
	case *pb.Packet_FwUpdateAbortRequest:
		return &pb.Packet{MessageId: &pb.Packet_FwUpdateAbortResponse{
			FwUpdateAbortResponse: &pb.FwUpdateAbortResponse{},
		}}, nil
	default:
		f.t.Fatalf("unexpected request %T", packet.MessageId)
		return nil, nil
	}
}

func TestRunFirmwareUpdate(t *testing.T) {
	image, err := parseFirmwareImage(firmwareFixture())
	if err != nil {
		t.Fatal(err)
	}
	transport := &fakeFirmwareTransport{t: t}
	var lastDone, lastTotal uint32
	if err := runFirmwareUpdate(transport, image, func(done, total uint32) {
		lastDone, lastTotal = done, total
	}); err != nil {
		t.Fatal(err)
	}
	if transport.begin == nil || transport.begin.Board != "barracuda" || transport.begin.Crc32 != image.CRC32 {
		t.Fatalf("unexpected begin request: %+v", transport.begin)
	}
	if transport.offset != image.Size() || transport.commits != 1 {
		t.Fatalf("transfer did not complete: offset=%d commits=%d", transport.offset, transport.commits)
	}
	if lastDone != image.Size() || lastTotal != image.Size() {
		t.Fatalf("unexpected progress: %d/%d", lastDone, lastTotal)
	}
}
