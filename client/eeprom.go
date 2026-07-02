package client

import (
	"fmt"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

// Serial-number record layout in the on-board EEPROM (whalepod_automation),
// matching the firmware and the legacy control_tool:
//
//	byte 0: magic (0xA5)
//	byte 1: length L of the ASCII serial (1..serialMaxLen)
//	bytes 2..2+L: the ASCII serial
const (
	serialMagic  = 0xA5
	serialOffset = 0
	serialMaxLen = 30
)

// EepromRead reads length bytes starting at address from the on-board EEPROM
// (present only on the whalepod_automation board). The firmware replies with an
// empty slice when the I2C transaction itself failed (distinct from a
// readable-but-blank EEPROM), which is returned here as an empty, non-nil error.
func (c *Client) EepromRead(address, length uint32) ([]byte, error) {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_EepromReadRequest{
		EepromReadRequest: &pb.EepromReadRequest{Address: address, Length: length},
	}})
	if err != nil {
		return nil, err
	}
	got, ok := resp.MessageId.(*pb.Packet_EepromReadResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return got.EepromReadResponse.GetData(), nil
}

// EepromWrite writes data starting at address to the on-board EEPROM. It returns
// an error if the device reports the write did not succeed.
func (c *Client) EepromWrite(address uint32, data []byte) error {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_EepromWriteRequest{
		EepromWriteRequest: &pb.EepromWriteRequest{Address: address, Data: data},
	}})
	if err != nil {
		return err
	}
	got, ok := resp.MessageId.(*pb.Packet_EepromWriteResponse)
	if !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	if !got.EepromWriteResponse.GetSuccess() {
		return fmt.Errorf("EEPROM write failed")
	}
	return nil
}

// GetSerial reads and decodes the board serial number from EEPROM. It returns a
// descriptive error if the EEPROM can't be read or no serial is programmed.
func (c *Client) GetSerial() (string, error) {
	data, err := c.EepromRead(serialOffset, 2+serialMaxLen)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("can't read EEPROM (I2C bus error) — check the IO-connector pins")
	}
	if len(data) < 2 || data[0] != serialMagic {
		return "", fmt.Errorf("no serial number programmed")
	}
	l := int(data[1])
	if l == 0 || l > serialMaxLen || 2+l > len(data) {
		return "", fmt.Errorf("serial record invalid (length byte = %d)", l)
	}
	return string(data[2 : 2+l]), nil
}

// SetSerial encodes and writes the board serial number to EEPROM.
func (c *Client) SetSerial(serial string) error {
	if len(serial) == 0 || len(serial) > serialMaxLen {
		return fmt.Errorf("serial must be 1..%d characters (got %d)", serialMaxLen, len(serial))
	}
	rec := make([]byte, 2+len(serial))
	rec[0] = serialMagic
	rec[1] = byte(len(serial))
	copy(rec[2:], serial)
	return c.EepromWrite(serialOffset, rec)
}
